package coordinator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type preliminary struct {
	Kind              string                       `json:"kind"`
	Identity          proof.Identity               `json:"identity"`
	Namespace         string                       `json:"namespace"`
	NamespaceUID      string                       `json:"namespace_uid"`
	ClusterUID        string                       `json:"cluster_uid"`
	StartedAt         time.Time                    `json:"started_at"`
	CompletedAt       *time.Time                   `json:"completed_at"`
	CleanupAttempts   int                          `json:"cleanup_attempts"`
	InventoryComplete bool                         `json:"inventory_complete"`
	Resources         []proof.Ref                  `json:"resources"`
	Coverage          map[string]proof.Observation `json:"coverage"`
	Verdict           string                       `json:"verdict"`
	SignatureState    string                       `json:"signature_state"`
}

func validateEvidence(data []byte, l *Ledger) error {
	if len(data) == 0 || len(data) > 900<<10 {
		return errors.New("evidence exceeds local staging limit or is empty")
	}
	var p preliminary
	if err := strict(data, &p); err != nil {
		return err
	}
	canonical, err := proof.Canonical(p)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return errors.New("evidence must be canonical JSON")
	}
	if p.Kind != "cleanup-evidence/v1" || p.Identity != l.Identity || p.Namespace != l.ResourceNamespace || p.NamespaceUID != l.ResourceUID || p.ClusterUID != l.ClusterUID {
		return errors.New("evidence identity/namespace does not match coordinator assignment")
	}
	if p.SignatureState != "unsigned" || (p.Verdict != "partial" && p.Verdict != "fail") {
		return errors.New("job evidence cannot claim a signed or passing receipt")
	}
	if p.StartedAt.IsZero() || p.StartedAt.Before(l.CreatedAt.Add(-time.Minute)) || p.StartedAt.After(time.Now().Add(time.Minute)) || p.CompletedAt == nil || p.CompletedAt.Before(p.StartedAt) || p.CompletedAt.After(time.Now().Add(time.Minute)) || p.CleanupAttempts < 1 {
		return errors.New("invalid evidence chronology")
	}
	if len(p.Coverage) != 5 || len(p.Resources) > 10000 {
		return errors.New("unexpected evidence shape")
	}
	failed := false
	for _, name := range []string{"workspace", "credentials", "resources", "logs", "runner_disposal"} {
		o, ok := p.Coverage[name]
		if !ok || (o.Observer != "job" && o.Observer != "none") || len(o.Reason) == 0 || len(o.Reason) > 1024 {
			return errors.New("job evidence cannot assert trusted observation")
		}
		switch o.Status {
		case "verified", "failed", "unsupported", "unobservable":
		default:
			return errors.New("invalid coverage status")
		}
		if o.Observer == "none" && o.Status != "unobservable" {
			return errors.New("missing observer cannot verify coverage")
		}
		if (name == "logs" || name == "runner_disposal") && o.Status != "unobservable" {
			return errors.New("job cannot observe complete logs or its own disposal")
		}
		failed = failed || o.Status == "failed"
	}
	if (p.Verdict == "fail") != failed {
		return errors.New("verdict does not match coverage")
	}
	for _, r := range p.Resources {
		if r.Namespace != l.ResourceNamespace || r.UID == "" || r.Name == "" || r.Resource == "" || r.Version == "" {
			return errors.New("resource reference outside assigned namespace; runner PV creation is disabled")
		}
	}
	return nil
}

// ValidateEvidence validates untrusted job evidence without elevating provenance.
func ValidateEvidence(data []byte, l *Ledger) error { return validateEvidence(data, l) }

func (c *Coordinator) collectStage(ctx context.Context, l *Ledger) error {
	stage, err := c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Get(ctx, "guard-stage", meta.GetOptions{})
	if err != nil {
		return err
	}
	if string(stage.UID) != l.StageUID {
		return terminalCollection(errors.New("staging object UID changed"))
	}
	dir, _ := c.runDir(l.Identity.Run)
	if raw := []byte(stage.Data["guard.json"]); len(raw) > 0 {
		if len(raw) > 16<<10 {
			return terminalCollection(errors.New("guard status exceeds limit"))
		}
		var shape map[string]any
		if err = strict(raw, &shape); err != nil {
			return terminalCollection(err)
		}
		if err = durable(filepath.Join(dir, "guard.json"), raw); err != nil {
			return err
		}
		l.GuardDigest = proof.Digest(raw)
	}
	raw := []byte(stage.Data["evidence.json"])
	if len(raw) == 0 {
		l.EvidenceStatus = "missing"
		return nil
	}
	if err = validateEvidence(raw, l); err != nil {
		l.EvidenceStatus = "rejected"
		return terminalCollection(err)
	}
	digest := proof.Digest(raw)
	if l.EvidenceDigest != "" && l.EvidenceDigest != digest {
		l.EvidenceStatus = "conflict"
		return terminalCollection(errors.New("staged evidence changed after durable collection"))
	}
	if err = durable(filepath.Join(dir, "cleanup-evidence.json"), raw); err != nil {
		return err
	}
	l.EvidenceStatus = "accepted-untrusted"
	l.EvidenceDigest = digest
	return nil
}

// Snapshot logs through the trusted Kubernetes API after observed termination.
// This stage is bounded staging, not complete-log attestation or immutable archive.
// Pod loss/API error/limit preserves an explicit gap and never claims coverage.
func (c *Coordinator) collectLogs(ctx context.Context, l *Ledger) error {
	if l.CollectorRequest != nil {
		return c.collectTrusted(ctx, l)
	}
	if l.PodUID == "" {
		l.LogsStatus = "missing"
		return nil
	}
	pod, err := c.K.CoreV1().Pods(l.RunnerNamespace).Get(ctx, l.PodName, meta.GetOptions{})
	if err != nil {
		l.LogsStatus = "missing"
		return err
	}
	if string(pod.UID) != l.PodUID {
		return terminalCollection(errors.New("pod UID changed before log retrieval"))
	}
	started := false
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "guard" && (status.State.Running != nil || status.State.Terminated != nil) {
			started = true
		}
	}
	if !started {
		l.LogsStatus = "missing"
		return nil
	}
	stream, err := c.K.CoreV1().Pods(l.RunnerNamespace).GetLogs(l.PodName, &core.PodLogOptions{Container: "guard", Timestamps: true}).Stream(ctx)
	if err != nil {
		l.LogsStatus = "gap"
		return err
	}
	defer stream.Close()
	dir, _ := c.runDir(l.Identity.Run)
	f, err := os.CreateTemp(dir, ".logs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	count, copyErr := io.Copy(f, io.LimitReader(stream, (100<<20)+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		l.LogsStatus = "gap"
		return errors.Join(copyErr, syncErr, closeErr)
	}
	if count > 100<<20 {
		l.LogsStatus = "limit-exceeded"
		return terminalCollection(fmt.Errorf("log staging exceeds 100 MiB"))
	}
	data, err := proof.ReadBounded(f.Name(), 100<<20)
	if err != nil {
		return err
	}
	if err = durable(filepath.Join(dir, "job.log"), data); err != nil {
		return err
	}
	l.LogsBytes = count
	l.LogsDigest = proof.Digest(data)
	l.LogsStatus = "snapshot-unattested"
	return nil
}
