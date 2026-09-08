// Package githubrun collects GitHub control-plane facts, never executes job code.
// Files published by this package must live in an operator-only input directory.
package githubrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

const Kind = "github-ci-run/v1"
const MaxEvidence = 900 << 10
const MaxLogs = 100 << 20

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Keep the complete artifact name under 120 bytes even at maximum run/attempt
// widths, avoiding the Action uploader's truncation collisions.
var jobPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,68}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidIdentity(id proof.Identity) bool {
	n, err := strconv.ParseInt(id.Run, 10, 64)
	return id.Validate() == nil && id.Provider == "github" && repoPattern.MatchString(id.Repository) && jobPattern.MatchString(id.Job) && err == nil && n > 0 && strconv.FormatInt(n, 10) == id.Run
}

// Directory is a path-safe identity key. Revision is excluded, so a changed
// revision at the same run/attempt/job becomes a conflict, not a second receipt.
func Directory(id proof.Identity) string {
	id.Revision = ""
	b, _ := proof.Canonical(id)
	return proof.Digest(b)[:32]
}

type Ledger struct {
	Kind           string         `json:"kind"`
	Identity       proof.Identity `json:"identity"`
	WorkflowID     int64          `json:"workflow_id"`
	WorkflowPath   string         `json:"workflow_path"`
	JobID          int64          `json:"github_job_id"`
	JobName        string         `json:"job_name"`
	Conclusion     string         `json:"conclusion"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    time.Time      `json:"completed_at"`
	EvidenceStatus string         `json:"evidence_status"`
	EvidenceDigest string         `json:"evidence_sha256"`
	LogsStatus     string         `json:"logs_status"`
	LogsDigest     string         `json:"logs_sha256"`
	LogsBytes      int64          `json:"logs_bytes"`
}

func (l *Ledger) Validate() error {
	if l.Kind != Kind || !ValidIdentity(l.Identity) || l.WorkflowID <= 0 || !regexp.MustCompile(`^\.github/workflows/[A-Za-z0-9_-]+\.ya?ml$`).MatchString(l.WorkflowPath) || l.JobID < 0 || l.JobName == "" || len(l.JobName) > 255 || l.Conclusion == "" || len(l.Conclusion) > 80 || l.StartedAt.IsZero() || l.CompletedAt.Before(l.StartedAt) {
		return errors.New("invalid GitHub control-plane binding")
	}
	if l.EvidenceStatus != "accepted-untrusted" && l.EvidenceStatus != "missing" && l.EvidenceStatus != "invalid" {
		return errors.New("invalid evidence state")
	}
	if l.EvidenceDigest != "" && !hashPattern.MatchString(l.EvidenceDigest) || l.EvidenceStatus == "accepted-untrusted" && l.EvidenceDigest == "" || l.EvidenceStatus == "missing" && l.EvidenceDigest != "" {
		return errors.New("invalid evidence digest")
	}
	if l.LogsStatus != "collected" && l.LogsStatus != "missing" && l.LogsStatus != "oversized" {
		return errors.New("invalid log state")
	}
	if l.LogsStatus == "collected" {
		if l.JobID == 0 || !hashPattern.MatchString(l.LogsDigest) || l.LogsBytes < 1 || l.LogsBytes > MaxLogs {
			return errors.New("invalid log binding")
		}
	} else if l.LogsDigest != "" || l.LogsBytes != 0 {
		return errors.New("unexpected log digest")
	}
	return nil
}

func Parse(data []byte) (*Ledger, error) {
	var l Ledger
	if len(data) > 1<<20 || json.Unmarshal(data, &l) != nil {
		return nil, errors.New("invalid GitHub ledger")
	}
	b, err := proof.Canonical(l)
	if err != nil || !bytes.Equal(b, data) {
		return nil, errors.New("noncanonical GitHub ledger")
	}
	return &l, l.Validate()
}

// Job-supplied Kubernetes UIDs are used only to check internal consistency of
// untrusted evidence. They never become trusted receipt allocation bindings.
func ValidateEvidence(data []byte, l *Ledger) error {
	var p struct {
		Namespace    string    `json:"namespace"`
		NamespaceUID string    `json:"namespace_uid"`
		ClusterUID   string    `json:"cluster_uid"`
		CompletedAt  time.Time `json:"completed_at"`
	}
	if json.Unmarshal(data, &p) != nil || !regexp.MustCompile(`^proof-[a-f0-9]{32}$`).MatchString(p.Namespace) || p.NamespaceUID == "" || p.ClusterUID == "" || len(p.NamespaceUID) > 255 || len(p.ClusterUID) > 255 || p.CompletedAt.After(l.CompletedAt.Add(time.Minute)) {
		return errors.New("invalid job evidence binding")
	}
	return coordinator.ValidateEvidence(data, &coordinator.Ledger{Identity: l.Identity, ResourceNamespace: p.Namespace, ResourceUID: p.NamespaceUID, ClusterUID: p.ClusterUID, CreatedAt: l.StartedAt})
}

func ValidateFiles(l *Ledger, files map[string][]byte) error {
	if err := l.Validate(); err != nil {
		return err
	}
	canonical, err := proof.Canonical(l)
	if err != nil || !bytes.Equal(canonical, files["run.json"]) {
		return errors.New("ledger bytes mismatch")
	}
	for name := range files {
		if name != "run.json" && name != "cleanup-evidence.json" && name != "job.log" && name != "finalization-ready.json" {
			return fmt.Errorf("unexpected GitHub input %s", name)
		}
	}
	evidence, exists := files["cleanup-evidence.json"]
	if exists != (l.EvidenceDigest != "") || exists && (len(evidence) > MaxEvidence || proof.Digest(evidence) != l.EvidenceDigest) {
		return errors.New("evidence snapshot mismatch")
	}
	if l.EvidenceStatus == "accepted-untrusted" && ValidateEvidence(evidence, l) != nil {
		return errors.New("invalid accepted evidence")
	}
	logs, exists := files["job.log"]
	if exists != (l.LogsStatus == "collected") || exists && (int64(len(logs)) != l.LogsBytes || proof.Digest(logs) != l.LogsDigest) {
		return errors.New("log snapshot mismatch")
	}
	return nil
}

func Ready(files map[string][]byte) []byte {
	digests := map[string]string{}
	for k, v := range files {
		if k != "finalization-ready.json" {
			digests[k] = proof.Digest(v)
		}
	}
	b, _ := proof.Canonical(struct {
		Kind  string            `json:"kind"`
		Files map[string]string `json:"files"`
	}{"github-ci-ready/v1", digests})
	return b
}
