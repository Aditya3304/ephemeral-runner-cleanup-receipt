package finalizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubrun"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"golang.org/x/sys/unix"
)

type Archiver interface {
	Put(context.Context, string, []byte) (archive.Ref, error)
	Verify(context.Context, archive.Ref) error
	Protect(context.Context, archive.Ref) error
}
type Engine struct {
	InputDir  string // Read-only coordinator root, containing <run_id>/run.json.
	StateDir  string // Private durable finalizer volume.
	OutputDir string
	Policy    SignerPolicy
	Archive   Archiver
	Signer    Signer
	Now       func() time.Time
}
type snapshot struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
}
type retryState struct {
	Kind          string                 `json:"kind"`
	Identity      proof.Identity         `json:"identity"`
	Policy        SignerPolicy           `json:"policy"`
	InputSHA256   string                 `json:"input_sha256"`
	Snapshots     map[string]snapshot    `json:"snapshots"`
	FinalizedAt   time.Time              `json:"finalized_at"`
	Attempts      int                    `json:"attempts"`
	Stage         string                 `json:"stage"`
	Status        string                 `json:"status"`
	NextRetryAt   *time.Time             `json:"next_retry_at"`
	ErrorCode     string                 `json:"error_code"`
	Objects       map[string]archive.Ref `json:"objects"`
	ReceiptSHA256 string                 `json:"receipt_sha256"`
	BundleSHA256  string                 `json:"bundle_sha256"`
}

var ErrConflict = errors.New("finalization identity/input/policy conflict")
var ErrRetryNotDue = errors.New("finalization retry is not due")
var ErrExhausted = errors.New("finalization exhausted six attempts; retained for operator inspection")
var ErrCoordinatorNotReady = errors.New("coordinator finalization publication is not committed; collect/retry required")
var artifactLimits = map[string]int64{"run.json": MaxReceipt, "observations.json": MaxReceipt, "cleanup-evidence.json": MaxReceipt, "job.log": MaxLogs, "finalization-ready.json": 16 << 10}

func durable(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func saveState(dir string, s *retryState) error {
	data, err := proof.Canonical(s)
	if err != nil {
		return err
	}
	return durable(filepath.Join(dir, "state.json"), data)
}
func privateDir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("finalizer directories must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if resolved != filepath.Clean(path) {
		return errors.New("finalizer directory must not traverse symlinks")
	}
	return nil
}
func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "finalizer.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid lock file")
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another finalizer is active")
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
func readInputs(dir, run string) (*coordinator.Ledger, map[string][]byte, map[string]snapshot, string, error) {
	data := map[string][]byte{}
	snaps := map[string]snapshot{}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != filepath.Clean(dir) {
		return nil, nil, nil, "", errors.New("input directory must not traverse symlinks")
	}
	ready, err := proof.ReadBounded(filepath.Join(dir, "finalization-ready.json"), 16<<10)
	if err != nil {
		return nil, nil, nil, "", ErrCoordinatorNotReady
	}
	for name, limit := range artifactLimits {
		b, err := proof.ReadBounded(filepath.Join(dir, name), limit)
		if err != nil {
			if name == "run.json" {
				return nil, nil, nil, "", errors.New("trusted coordinator ledger unavailable")
			}
			status := "unreadable"
			if errors.Is(err, os.ErrNotExist) {
				status = "missing"
			}
			snaps[name] = snapshot{Status: status}
			continue
		}
		data[name] = b
		snaps[name] = snapshot{SHA256: proof.Digest(b), Size: int64(len(b)), Status: "present"}
	}
	var l coordinator.Ledger
	var envelope struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(data["run.json"], &envelope)
	if envelope.Kind == githubrun.Kind {
		g, err := githubrun.Parse(data["run.json"])
		if err != nil {
			return nil, nil, nil, "", err
		}
		if githubrun.Directory(g.Identity) != run || githubrun.ValidateFiles(g, data) != nil || !bytes.Equal(ready, data["finalization-ready.json"]) || !bytes.Equal(ready, githubrun.Ready(data)) {
			return nil, nil, nil, "", errors.New("GitHub publication binding mismatch")
		}
		b, err := proof.Canonical(snaps)
		return githubLedger(g), data, snaps, proof.Digest(b), err
	}
	if err := strictCanonical(data["run.json"], MaxReceipt, &l); err != nil {
		return nil, nil, nil, "", fmt.Errorf("invalid trusted ledger: %w", err)
	}
	if l.Kind != "local-ci-run/v1" || l.Token != run || l.Identity.Run != run || l.Identity.Provider != "local" || l.ResourceNamespace != "proof-"+run || l.RunnerNamespace != "proof-runner-"+run || l.Phase != "complete" || l.CreatedAt.IsZero() || l.UpdatedAt.Before(l.CreatedAt) {
		return nil, nil, nil, "", errors.New("coordinator must publish a complete bound ledger")
	}
	if err := l.Identity.Validate(); err != nil {
		return nil, nil, nil, "", err
	}
	if !bytes.Equal(ready, data["finalization-ready.json"]) {
		return nil, nil, nil, "", ErrCoordinatorNotReady
	}
	if err := coordinator.ValidateFinalizationReady(ready, data["run.json"], data["observations.json"]); err != nil {
		return nil, nil, nil, "", fmt.Errorf("%w: readiness hash/identity mismatch", ErrCoordinatorNotReady)
	}
	b, err := proof.Canonical(snaps)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return &l, data, snaps, proof.Digest(b), nil
}

// Run freezes the first complete coordinator snapshot. A changed input at the
// same deduplication identity is an explicit conflict, including a new revision.
func (e *Engine) Run(ctx context.Context, run string) (*Result, error) {
	if !tokenPattern.MatchString(run) || e.Archive == nil || e.Signer == nil {
		return nil, errors.New("run, archive and signer required")
	}
	if !filepath.IsAbs(e.InputDir) {
		return nil, errors.New("coordinator root must be absolute")
	}
	for _, dir := range []string{e.StateDir, e.OutputDir} {
		if err := privateDir(dir); err != nil {
			return nil, err
		}
	}
	// Operator mount configuration must also enforce this separation in the container.
	for _, a := range []string{e.StateDir, e.OutputDir} {
		rel, _ := filepath.Rel(e.InputDir, a)
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil, errors.New("writable finalizer directory overlaps coordinator inputs")
		}
	}
	unlock, err := lock(e.StateDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	l, input, snaps, inputDigest, err := readInputs(filepath.Join(e.InputDir, run), run)
	if err != nil {
		return nil, err
	}
	// Source revision is deliberately excluded from the deduplication key.
	keyID := l.Identity
	keyID.Revision = ""
	keyBytes, _ := proof.Canonical(keyID)
	key := proof.Digest(keyBytes)
	dir := filepath.Join(e.StateDir, key)
	if err = privateDir(dir); err != nil {
		return nil, err
	}
	statePath := filepath.Join(dir, "state.json")
	var state retryState
	b, err := proof.ReadBounded(statePath, MaxReceipt)
	if errors.Is(err, os.ErrNotExist) {
		state = retryState{Kind: "cleanup-finalization-state/v1", Identity: l.Identity, Policy: e.Policy, InputSHA256: inputDigest, Snapshots: snaps, FinalizedAt: e.now(), Objects: map[string]archive.Ref{}, Status: "pending", Stage: "archive"}
		if state.FinalizedAt.Before(l.UpdatedAt) {
			return nil, errors.New("coordinator chronology is in the future")
		}
		for name, data := range input {
			if err = durable(filepath.Join(dir, name), data); err != nil {
				return nil, err
			}
		}
		if err = saveState(dir, &state); err != nil {
			return nil, err
		}
		if err = syncDir(e.StateDir); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if err = strictCanonical(b, MaxReceipt, &state); err != nil {
			return nil, err
		}
		if state.Kind != "cleanup-finalization-state/v1" || state.Identity != l.Identity || state.Policy != e.Policy || state.InputSHA256 != inputDigest {
			return nil, ErrConflict
		}
		if state.Attempts < 0 || state.Attempts > 6 || state.Objects == nil || len(state.Objects) > 6 || len(state.Snapshots) != len(artifactLimits) {
			return nil, errors.New("invalid retry state")
		}
	}
	// Re-read only private snapshots from this point. Detect corruption on restart.
	input = map[string][]byte{}
	for name, snap := range state.Snapshots {
		limit, ok := artifactLimits[name]
		if !ok || snap != snaps[name] {
			return nil, ErrConflict
		}
		if snap.Status == "present" {
			b, err := proof.ReadBounded(filepath.Join(dir, name), limit)
			if err != nil || proof.Digest(b) != snap.SHA256 || int64(len(b)) != snap.Size {
				return nil, errors.New("durable snapshot tampered")
			}
			input[name] = b
		}
	}
	if state.Status == "complete" {
		return e.completed(ctx, dir, &state, false)
	}
	// Completed archival can be reconciled without another signing/upload attempt.
	// This must precede exhaustion: a process can die after the sixth publication
	// but before persisting the complete state. Reverify all bytes before recovery.
	_, receiptArchived := state.Objects["receipt.json"]
	_, bundleArchived := state.Objects["receipt.bundle.json"]
	if receiptArchived && bundleArchived && state.ReceiptSHA256 != "" && state.BundleSHA256 != "" {
		if state.NextRetryAt != nil && e.now().Before(*state.NextRetryAt) {
			return nil, ErrRetryNotDue
		}
		result, recoverErr := e.completed(ctx, dir, &state, true)
		if recoverErr != nil {
			state.Status = "recovering"
			state.ErrorCode = "reconcile_failed"
			due := e.now().Add(time.Minute)
			state.NextRetryAt = &due
			return nil, errors.Join(recoverErr, saveState(dir, &state))
		}
		state.Status = "complete"
		state.Stage = "complete"
		state.NextRetryAt = nil
		state.ErrorCode = ""
		if err = saveState(dir, &state); err != nil {
			return nil, err
		}
		return result, nil
	}
	if state.Status == "exhausted" || state.Attempts >= 6 {
		state.Status = "exhausted"
		state.NextRetryAt = nil
		if err = saveState(dir, &state); err != nil {
			return nil, err
		}
		return nil, ErrExhausted
	}
	if state.NextRetryAt != nil && e.now().Before(*state.NextRetryAt) {
		return nil, ErrRetryNotDue
	}
	state.Attempts++
	state.Status = "running"
	state.NextRetryAt = nil
	state.ErrorCode = ""
	if err = saveState(dir, &state); err != nil {
		return nil, err
	}
	result, err := e.attempt(ctx, dir, l, input, &state)
	if err != nil {
		// Persist only bounded machine codes, never service diagnostics or secrets.
		state.ErrorCode = state.Stage + "_failed"
		state.Status = "retry"
		if state.Attempts >= 6 {
			state.Status = "exhausted"
		} else {
			delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
			due := e.now().Add(delays[state.Attempts-1])
			state.NextRetryAt = &due
		}
		return nil, errors.Join(err, saveState(dir, &state))
	}
	return result, nil
}
func (e *Engine) put(ctx context.Context, dir, name string, data []byte, s *retryState) (archive.Ref, error) {
	if ref, ok := s.Objects[name]; ok {
		if err := validateRef(ref); err != nil {
			return archive.Ref{}, err
		}
		if ref.SHA256 != proof.Digest(data) || ref.SizeBytes != int64(len(data)) {
			return archive.Ref{}, errors.New("archived reference conflicts with snapshot")
		}
		if err := e.Archive.Verify(ctx, ref); err != nil {
			return archive.Ref{}, err
		}
		if err := e.Archive.Protect(ctx, ref); err != nil {
			return archive.Ref{}, err
		}
		return ref, nil
	}
	ref, err := e.Archive.Put(ctx, name, data)
	if err != nil {
		return ref, err
	}
	if err = validateRef(ref); err != nil {
		return ref, err
	}
	if ref.SHA256 != proof.Digest(data) || ref.SizeBytes != int64(len(data)) {
		return ref, errors.New("archiver returned different bytes")
	}
	if err = e.Archive.Verify(ctx, ref); err != nil {
		return ref, err
	}
	s.Objects[name] = ref
	return ref, saveState(dir, s)
}
func boundedReason(s string) string {
	// Observation summaries are metadata; archived raw bytes preserve full context.
	out := []rune{}
	for _, r := range s {
		if !unicode.IsControl(r) {
			out = append(out, r)
		}
		if len(string(out)) > 480 {
			out = out[:len(out)-1]
			break
		}
	}
	if len(out) == 0 {
		return "Observation recorded; see archived evidence"
	}
	return string(out)
}
func buildReceipt(l *coordinator.Ledger, input map[string][]byte, s *retryState) (*Receipt, error) {
	if l.Kind == githubrun.Kind {
		return buildGitHubReceipt(input, s)
	}
	r := &Receipt{Kind: "cleanup-receipt/v1", Identity: l.Identity, Binding: Binding{l.ClusterUID, l.ResourceNamespace, l.ResourceUID, l.RunnerNamespace, l.RunnerUID, l.JobUID, l.PodUID, proof.Digest(input["run.json"])}, Finalizer: s.Policy, StartedAt: l.CreatedAt, CompletedAt: l.UpdatedAt, FinalizedAt: s.FinalizedAt, EvidenceStatus: "missing", Coverage: map[string]proof.Observation{}, Objects: map[string]archive.Ref{}, LogObjects: []archive.Ref{}}
	for _, k := range components {
		r.Coverage[k] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "No valid independent observation available"}
	}
	evidence := input["cleanup-evidence.json"]
	if s.Snapshots["cleanup-evidence.json"].Status == "unreadable" {
		r.EvidenceStatus = "invalid"
	}
	if evidence != nil {
		r.EvidenceStatus = "invalid"
		if l.EvidenceStatus == "accepted-untrusted" && proof.Digest(evidence) == l.EvidenceDigest && coordinator.ValidateEvidence(evidence, l) == nil {
			var p struct {
				Coverage map[string]proof.Observation `json:"coverage"`
			}
			if err := json.Unmarshal(evidence, &p); err != nil {
				return nil, err
			}
			r.EvidenceStatus = "accepted-untrusted"
			for k, o := range p.Coverage {
				o.Reason = boundedReason(o.Reason)
				r.Coverage[k] = o
			}
		}
	}
	if observed := input["observations.json"]; observed != nil {
		o, err := coordinator.ValidateObservations(observed, l, input["run.json"])
		if err == nil {
			for _, k := range components {
				v := o.Coverage[k]
				v.Reason = boundedReason(v.Reason)
				if r.Coverage[k].Status == "failed" {
					continue
				}
				if v.Observer == "trusted" || r.Coverage[k].Observer == "none" {
					r.Coverage[k] = v
				}
			}
		}
	}
	for name, ref := range s.Objects {
		switch name {
		case "run.json", "observations.json", "cleanup-evidence.json":
			r.Objects[name] = ref
		case "job.log":
			r.LogObjects = append(r.LogObjects, ref)
		}
	}
	logs := input["job.log"]
	if logs == nil || proof.Digest(logs) != l.LogsDigest || int64(len(logs)) != l.LogsBytes || len(r.LogObjects) == 0 {
		if r.Coverage["logs"].Status != "failed" {
			r.Coverage["logs"] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "Log snapshot missing or does not match coordinator digest and size"}
		}
	}
	// Invalid/missing job evidence must remain explicit even when independent
	// disposal observations exist. Preserve every observed failure.
	if r.EvidenceStatus != "accepted-untrusted" && r.Coverage["workspace"].Status != "failed" {
		r.Coverage["workspace"] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "Preliminary cleanup evidence is " + r.EvidenceStatus}
	}
	r.Verdict = Verdict(r.Coverage)
	return r, r.Validate()
}
func (e *Engine) attempt(ctx context.Context, dir string, l *coordinator.Ledger, input map[string][]byte, s *retryState) (*Result, error) {
	s.Stage = "archive"
	if err := saveState(dir, s); err != nil {
		return nil, err
	}
	names := []string{}
	for name := range input {
		if name == "finalization-ready.json" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := e.put(ctx, dir, name, input[name], s); err != nil {
			return nil, err
		}
	}
	r, err := buildReceipt(l, input, s)
	if err != nil {
		return nil, err
	}
	receipt, err := r.Canonical()
	if err != nil {
		return nil, err
	}
	if s.ReceiptSHA256 != "" && s.ReceiptSHA256 != proof.Digest(receipt) {
		return nil, ErrConflict
	}
	if err = durable(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		return nil, err
	}
	s.ReceiptSHA256 = proof.Digest(receipt)
	s.Stage = "sign"
	if err = saveState(dir, s); err != nil {
		return nil, err
	}
	var bundle []byte
	if s.BundleSHA256 != "" {
		bundle, err = proof.ReadBounded(filepath.Join(dir, "receipt.bundle.json"), MaxBundle)
		if err != nil || proof.Digest(bundle) != s.BundleSHA256 {
			return nil, errors.New("durable bundle tampered")
		}
	} else {
		bundle, err = e.Signer.Sign(ctx, receipt)
		if err != nil {
			return nil, err
		}
		if len(bundle) == 0 || len(bundle) > MaxBundle {
			return nil, errors.New("invalid bundle size")
		}
		// Persist before verification to recover transient verification/storage errors.
		if err = durable(filepath.Join(dir, "receipt.bundle.json"), bundle); err != nil {
			return nil, err
		}
		s.BundleSHA256 = proof.Digest(bundle)
		if err = saveState(dir, s); err != nil {
			return nil, err
		}
	}
	s.Stage = "verify"
	if err = saveState(dir, s); err != nil {
		return nil, err
	}
	if err = e.Signer.Verify(ctx, receipt, bundle); err != nil {
		return nil, err
	}
	s.Stage = "archive_signed"
	if err = saveState(dir, s); err != nil {
		return nil, err
	}
	if _, err = e.put(ctx, dir, "receipt.json", receipt, s); err != nil {
		return nil, err
	}
	if _, err = e.put(ctx, dir, "receipt.bundle.json", bundle, s); err != nil {
		return nil, err
	}
	s.Stage = "publish"
	if err = saveState(dir, s); err != nil {
		return nil, err
	}
	result := &Result{Kind: "cleanup-finalization-result/v1", Identity: s.Identity, Receipt: s.Objects["receipt.json"], Bundle: s.Objects["receipt.bundle.json"], SignatureState: "verified"}
	if err = e.protect(ctx, s); err != nil {
		return nil, err
	}
	if err = e.publish(dir, result, receipt, bundle); err != nil {
		return nil, err
	}
	s.Stage = "complete"
	s.Status = "complete"
	s.NextRetryAt = nil
	s.ErrorCode = ""
	if err = saveState(dir, s); err != nil {
		return nil, err
	}
	return result, nil
}
func (e *Engine) publish(dir string, result *Result, receipt, bundle []byte) error {
	out := filepath.Join(e.OutputDir, filepath.Base(dir))
	if err := privateDir(out); err != nil {
		return err
	}
	for name, b := range map[string][]byte{"receipt.json": receipt, "receipt.bundle.json": bundle} {
		if err := durable(filepath.Join(out, name), b); err != nil {
			return err
		}
	}
	// result.json is the last publication marker. Readers require this marker.
	b, err := proof.Canonical(result)
	if err != nil {
		return err
	}
	if err = durable(filepath.Join(out, "result.json"), b); err != nil {
		return err
	}
	return syncDir(e.OutputDir)
}
func (e *Engine) protect(ctx context.Context, s *retryState) error {
	for _, ref := range s.Objects {
		if err := e.Archive.Protect(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) completed(ctx context.Context, dir string, s *retryState, admitting bool) (*Result, error) {
	receipt, err := proof.ReadBounded(filepath.Join(dir, "receipt.json"), MaxReceipt)
	if err != nil || proof.Digest(receipt) != s.ReceiptSHA256 {
		return nil, errors.New("completed receipt tampered")
	}
	bundle, err := proof.ReadBounded(filepath.Join(dir, "receipt.bundle.json"), MaxBundle)
	if err != nil || proof.Digest(bundle) != s.BundleSHA256 {
		return nil, errors.New("completed bundle tampered")
	}
	r, err := ParseReceipt(receipt)
	if err != nil {
		return nil, err
	}
	if r.Identity != s.Identity || r.Finalizer != s.Policy {
		return nil, ErrConflict
	}
	if err = e.Signer.Verify(ctx, receipt, bundle); err != nil {
		return nil, err
	}
	for name, ref := range s.Objects {
		if err = validateRef(ref); err != nil {
			return nil, err
		}
		if name == "receipt.json" && ref.SHA256 != s.ReceiptSHA256 {
			return nil, ErrConflict
		}
		if name == "receipt.bundle.json" && ref.SHA256 != s.BundleSHA256 {
			return nil, ErrConflict
		}
		if err = e.Archive.Verify(ctx, ref); err != nil {
			return nil, err
		}
	}
	result := &Result{Kind: "cleanup-finalization-result/v1", Identity: s.Identity, Receipt: s.Objects["receipt.json"], Bundle: s.Objects["receipt.bundle.json"], SignatureState: "verified"}
	if err = validateRef(result.Receipt); err != nil {
		return nil, err
	}
	if err = validateRef(result.Bundle); err != nil {
		return nil, err
	}
	if admitting {
		if err = e.protect(ctx, s); err != nil {
			return nil, err
		}
	}
	return result, e.publish(dir, result, receipt, bundle)
}
