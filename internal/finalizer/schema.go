// Package finalizer consumes operator-owned coordinator snapshots, never job code.
package finalizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const MaxReceipt = 1 << 20
const MaxBundle = 2 << 20
const MaxLogs = 100 << 20

var components = []string{"workspace", "credentials", "resources", "logs", "runner_disposal"}
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Binding is copied only from the trusted coordinator ledger. Empty allocation
// UIDs describe an allocation that never completed, and cannot support a pass.
type Binding struct {
	ClusterUID        string `json:"cluster_uid"`
	ResourceNamespace string `json:"resource_namespace"`
	ResourceUID       string `json:"resource_uid"`
	RunnerNamespace   string `json:"runner_namespace"`
	RunnerUID         string `json:"runner_uid"`
	JobUID            string `json:"job_uid"`
	PodUID            string `json:"pod_uid"`
	LedgerSHA256      string `json:"ledger_sha256"`
}
type SignerPolicy struct {
	Revision            string `json:"revision"`
	Issuer              string `json:"issuer"`
	Identity            string `json:"identity"`
	TrustRootSHA256     string `json:"trust_root_sha256"`
	CosignSHA256        string `json:"cosign_sha256"`
	SigningConfigSHA256 string `json:"signing_config_sha256"`
}

// Receipt excludes its own digest and bundle: the detached Result binds both.
// All fields are required, including empty arrays/objects; null is never valid.
type Receipt struct {
	Kind           string                       `json:"kind"`
	Identity       proof.Identity               `json:"identity"`
	Binding        Binding                      `json:"binding"`
	Finalizer      SignerPolicy                 `json:"finalizer"`
	StartedAt      time.Time                    `json:"started_at"`
	CompletedAt    time.Time                    `json:"completed_at"`
	FinalizedAt    time.Time                    `json:"finalized_at"`
	EvidenceStatus string                       `json:"evidence_status"`
	Coverage       map[string]proof.Observation `json:"coverage"`
	Objects        map[string]archive.Ref       `json:"objects"`
	LogObjects     []archive.Ref                `json:"log_objects"`
	Verdict        string                       `json:"verdict"`
}
type Result struct {
	Kind           string         `json:"kind"`
	Identity       proof.Identity `json:"identity"`
	Receipt        archive.Ref    `json:"receipt"`
	Bundle         archive.Ref    `json:"bundle"`
	SignatureState string         `json:"signature_state"`
}

// Expected comes from independent operator/coordinator records, never the receipt.
type Expected struct {
	Identity proof.Identity
	Binding  Binding
	Policy   SignerPolicy
}

func VerifyReceipt(ctx context.Context, data, bundle []byte, expected Expected, signer Signer) (*Receipt, error) {
	r, err := ParseReceipt(data)
	if err != nil {
		return nil, err
	}
	if r.Identity != expected.Identity || r.Binding != expected.Binding || r.Finalizer != expected.Policy {
		return nil, errors.New("receipt identity, UID, source revision or signer policy mismatch")
	}
	if signer == nil {
		return nil, errors.New("signature verifier required")
	}
	if err = signer.Verify(ctx, data, bundle); err != nil {
		return nil, err
	}
	return r, nil
}

func safeText(s string, max int, empty bool) bool {
	if (!empty && s == "") || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validateRef(r archive.Ref) error {
	if !safeText(r.Store, 80, false) || strings.ContainsAny(r.Store, "/:\\") || !safeText(r.Bucket, 63, false) || strings.ContainsAny(r.Bucket, "/:\\") || !safeText(r.Key, 1024, false) || strings.HasPrefix(r.Key, "/") || strings.Contains(r.Key, "://") || !safeText(r.VersionID, 1024, false) || r.VersionID == "null" || !hashPattern.MatchString(r.SHA256) || r.SizeBytes < 0 || r.SizeBytes > MaxLogs {
		return errors.New("invalid immutable archive reference")
	}
	for _, p := range strings.Split(r.Key, "/") {
		if p == ".." || p == "." || p == "" {
			return errors.New("invalid archive key segment")
		}
	}
	return nil
}
func validateCoverage(c map[string]proof.Observation) error {
	if len(c) != len(components) {
		return errors.New("coverage must contain exactly five components")
	}
	for _, k := range components {
		o, ok := c[k]
		if !ok || !safeText(o.Reason, 512, false) {
			return fmt.Errorf("invalid coverage: %s", k)
		}
		if o.Status != "verified" && o.Status != "failed" && o.Status != "unsupported" && o.Status != "unobservable" {
			return errors.New("invalid coverage status")
		}
		if o.Observer != "trusted" && o.Observer != "job" && o.Observer != "none" {
			return errors.New("invalid observer")
		}
		if o.Observer == "none" && o.Status != "unobservable" {
			return errors.New("absent observer cannot attest")
		}
	}
	return nil
}
func Verdict(c map[string]proof.Observation) string {
	all := true
	for _, k := range components {
		o := c[k]
		if o.Status == "failed" {
			return "fail"
		}
		all = all && o.Status == "verified" && o.Observer == "trusted"
	}
	if all {
		return "pass"
	}
	return "partial"
}
func (r *Receipt) Validate() error {
	if r.Kind != "cleanup-receipt/v1" {
		return errors.New("unsupported receipt kind")
	}
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	for _, s := range []string{r.Identity.Repository, r.Identity.Run, r.Identity.Job} {
		if !safeText(s, 255, false) {
			return errors.New("invalid identity text")
		}
	}
	if r.Identity.Provider != "local" || !tokenPattern.MatchString(r.Identity.Run) || r.Binding.ResourceNamespace != "proof-"+r.Identity.Run || r.Binding.RunnerNamespace != "proof-runner-"+r.Identity.Run {
		return errors.New("invalid local assignment")
	}
	for _, s := range []string{r.Binding.ClusterUID, r.Binding.ResourceUID, r.Binding.RunnerUID, r.Binding.JobUID, r.Binding.PodUID} {
		if !safeText(s, 255, true) {
			return errors.New("invalid UID")
		}
	}
	if !hashPattern.MatchString(r.Binding.LedgerSHA256) || !revisionPattern.MatchString(r.Finalizer.Revision) || !hashPattern.MatchString(r.Finalizer.TrustRootSHA256) || !hashPattern.MatchString(r.Finalizer.CosignSHA256) || !hashPattern.MatchString(r.Finalizer.SigningConfigSHA256) || !safeText(r.Finalizer.Issuer, 1024, false) || !safeText(r.Finalizer.Identity, 1024, false) {
		return errors.New("invalid finalizer binding")
	}
	if r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.FinalizedAt.Before(r.CompletedAt) {
		return errors.New("invalid chronology")
	}
	if r.EvidenceStatus != "accepted-untrusted" && r.EvidenceStatus != "missing" && r.EvidenceStatus != "invalid" {
		return errors.New("invalid evidence status")
	}
	if err := validateCoverage(r.Coverage); err != nil {
		return err
	}
	if r.Verdict != Verdict(r.Coverage) {
		return errors.New("verdict does not match coverage")
	}
	if r.Objects == nil || r.LogObjects == nil || len(r.Objects) > 3 || len(r.LogObjects) > 1 {
		return errors.New("invalid archive collections")
	}
	if _, ok := r.Objects["run.json"]; !ok {
		return errors.New("ledger archive required")
	}
	for k, ref := range r.Objects {
		if k != "run.json" && k != "observations.json" && k != "cleanup-evidence.json" {
			return errors.New("unknown archived artifact")
		}
		if err := validateRef(ref); err != nil {
			return err
		}
		if ref.SizeBytes > MaxReceipt {
			return errors.New("oversized JSON object")
		}
	}
	if r.Objects["run.json"].SHA256 != r.Binding.LedgerSHA256 {
		return errors.New("ledger archive digest mismatch")
	}
	if r.EvidenceStatus == "accepted-untrusted" {
		if _, ok := r.Objects["cleanup-evidence.json"]; !ok {
			return errors.New("accepted evidence needs archive")
		}
	}
	for _, ref := range r.LogObjects {
		if err := validateRef(ref); err != nil {
			return err
		}
	}
	if r.Coverage["logs"].Status == "verified" && len(r.LogObjects) == 0 {
		return errors.New("verified logs need archive")
	}
	if r.Verdict == "pass" {
		if r.EvidenceStatus != "accepted-untrusted" || len(r.LogObjects) == 0 || r.Binding.ClusterUID == "" || r.Binding.ResourceUID == "" || r.Binding.RunnerUID == "" || r.Binding.JobUID == "" || r.Binding.PodUID == "" {
			return errors.New("pass lacks evidence or allocation binding")
		}
		if _, ok := r.Objects["observations.json"]; !ok {
			return errors.New("pass needs trusted observation archive")
		}
	}
	return nil
}

// strictCanonical also rejects missing fields, nulls, alternate JSON field case,
// duplicate keys, numeric coercions and noncanonical whitespace/ordering.
func strictCanonical(data []byte, limit int, out any) error {
	if len(data) == 0 || len(data) > limit || !utf8.Valid(data) {
		return errors.New("JSON exceeds bounds or is not UTF-8")
	}
	if _, err := jcs.Transform(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	canonical, err := proof.Canonical(out)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return errors.New("JSON must have exact required canonical shape")
	}
	return nil
}
func ParseReceipt(data []byte) (*Receipt, error) {
	var r Receipt
	if err := strictCanonical(data, MaxReceipt, &r); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}
func (r *Receipt) Canonical() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	b, err := proof.Canonical(r)
	if len(b) > MaxReceipt {
		return nil, errors.New("receipt exceeds 1 MiB")
	}
	return b, err
}
