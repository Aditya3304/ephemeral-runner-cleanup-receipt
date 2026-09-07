package coordinator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator/observer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

// Observations must be obtained from the private coordinator directory. A hash
// detects changes; it does not authenticate arbitrary caller-supplied host JSON.
type Observations struct {
	Kind           string                       `json:"kind"`
	Identity       proof.Identity               `json:"identity"`
	LedgerDigest   string                       `json:"ledger_sha256"`
	Coverage       map[string]proof.Observation `json:"coverage"`
	LogsDigest     string                       `json:"logs_sha256"`
	LogsBytes      int64                        `json:"logs_bytes"`
	EvidenceDigest string                       `json:"evidence_sha256"`
	GuardDigest    string                       `json:"guard_sha256"`
	Collector      *observer.Result             `json:"collector,omitempty"`
}

// FinalizationReady is published atomically LAST. Neither input contains this
// marker's digest, so there is no circular binding. Absence means retry, never
// permission to freeze a terminal missing-observations receipt.
type FinalizationReady struct {
	Kind               string         `json:"kind"`
	Identity           proof.Identity `json:"identity"`
	LedgerDigest       string         `json:"ledger_sha256"`
	ObservationsDigest string         `json:"observations_sha256"`
}

func ValidateFinalizationReady(data, ledgerData, observationsData []byte) error {
	if len(data) == 0 || len(data) > 4096 {
		return errors.New("invalid finalization readiness size")
	}
	var l Ledger
	if err := strict(ledgerData, &l); err != nil {
		return err
	}
	if _, err := ValidateObservations(observationsData, &l, ledgerData); err != nil {
		return err
	}
	want, err := proof.Canonical(FinalizationReady{Kind: "local-ci-ready/v1", Identity: l.Identity, LedgerDigest: proof.Digest(ledgerData), ObservationsDigest: proof.Digest(observationsData)})
	if err != nil {
		return err
	}
	if !bytes.Equal(data, want) {
		return errors.New("finalization readiness binding mismatch")
	}
	return nil
}

func publishImmutable(path string, data []byte, limit int64) error {
	existing, err := proof.ReadBounded(path, limit)
	if err == nil {
		if !bytes.Equal(existing, data) {
			return errors.New("immutable observation publication conflict")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return durable(path, data)
}

func observations(l *Ledger, ledgerData []byte, result *observer.Result) (*Observations, error) {
	var decoded Ledger
	if e := strict(ledgerData, &decoded); e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(l, &decoded) || l.Phase != "complete" || l.Identity.Provider != "local" {
		return nil, errors.New("observations require the exact completed local ledger")
	}
	o := &Observations{Kind: "local-ci-observations/v1", Identity: l.Identity, LedgerDigest: proof.Digest(ledgerData), LogsDigest: l.LogsDigest, LogsBytes: l.LogsBytes, EvidenceDigest: l.EvidenceDigest, GuardDigest: l.GuardDigest, Collector: result, Coverage: map[string]proof.Observation{}}
	for _, name := range []string{"workspace", "credentials", "resources", "logs", "runner_disposal"} {
		o.Coverage[name] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "Required trusted observation unavailable"}
	}
	if l.ResourceAbsent && l.ResourceUID != "" && l.ClusterUID != "" {
		o.Coverage["resources"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "Coordinator confirmed assigned namespace absent using pinned cluster and namespace UIDs; runner profile cannot provision volumes"}
	}
	disposed := l.RunnerAbsent && l.RunnerUID != "" && l.PodUID != "" && l.JobUID != "" && l.ClusterUID != "" && l.RoleAbsent && l.BindingAbsent && l.RoleUID != "" && l.BindingUID != ""
	if disposed {
		o.Coverage["runner_disposal"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "Coordinator confirmed runner namespace and run-scoped cluster RBAC absent after UID-bound disposal"}
	}
	if result != nil {
		if e := result.Validate(); e != nil {
			return nil, e
		}
		canonical, e := proof.Canonical(result)
		if e != nil {
			return nil, e
		}
		if l.CollectorRequest == nil || result.Request != *l.CollectorRequest || result.Request.Binding != collectorBinding(l) || proof.Digest(canonical) != l.CollectorDigest || result.LogsDigest != l.LogsDigest || result.LogsBytes != l.LogsBytes {
			return nil, errors.New("collector does not match final ledger")
		}
		if result.TerminationProven && (l.ContainerID != "containerd://"+result.ContainerID || l.ActualImage != result.ImageID || l.PodExitCode == nil || *l.PodExitCode != result.ExitCode) {
			return nil, errors.New("collector/API runtime identity mismatch")
		}
		if (result.Logs.Status == "verified") != (l.LogsStatus == "complete") {
			return nil, errors.New("log coverage mismatch")
		}
		for name, f := range map[string]observer.Finding{"workspace": result.Workspace, "credentials": result.Credentials, "logs": result.Logs} {
			o.Coverage[name] = proof.Observation{Status: f.Status, Observer: "trusted", Reason: f.Reason}
		}
		if result.Credentials.Status == "verified" {
			if disposed {
				o.Coverage["credentials"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "Stopped-runner credential directory absent/empty; coordinator also confirmed runner namespace and run-scoped RBAC disposal; no external credential revocation claimed"}
			} else {
				o.Coverage["credentials"] = proof.Observation{Status: "unobservable", Observer: "trusted", Reason: "Credential files inspected but runner token/RBAC disposal not fully confirmed"}
			}
		}
	} else if l.CollectorDigest != "" || l.LogsStatus == "complete" {
		return nil, errors.New("complete collector artifact missing")
	}
	return o, nil
}

// ValidateObservations checks canonical form, exact ledger binding and derived
// coverage. It does not re-read artifacts; LoadObservations additionally does so.
func ValidateObservations(data []byte, l *Ledger, ledgerData []byte) (*Observations, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("observation size limit")
	}
	var o Observations
	if e := strict(data, &o); e != nil {
		return nil, e
	}
	expected, e := observations(l, ledgerData, o.Collector)
	if e != nil {
		return nil, e
	}
	canonical, e := proof.Canonical(expected)
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(data, canonical) {
		return nil, errors.New("noncanonical or inconsistent trusted observations")
	}
	return &o, nil
}
func (c *Coordinator) publishObservations(l *Ledger) error {
	dir, e := c.runDir(l.Identity.Run)
	if e != nil {
		return e
	}
	ledgerData, e := proof.ReadBounded(filepath.Join(dir, "run.json"), 1<<20)
	if e != nil {
		return e
	}
	var result *observer.Result
	if l.CollectorDigest != "" {
		raw, e := proof.ReadBounded(filepath.Join(dir, "collector.json"), 64<<10)
		if e != nil {
			return e
		}
		if proof.Digest(raw) != l.CollectorDigest {
			return errors.New("collector digest mismatch")
		}
		result = &observer.Result{}
		if e = strict(raw, result); e != nil {
			return e
		}
	}
	o, e := observations(l, ledgerData, result)
	if e != nil {
		return e
	}
	if e = checkArtifacts(dir, l); e != nil {
		return e
	}
	raw, e := proof.Canonical(o)
	if e != nil {
		return e
	}
	if e = publishImmutable(filepath.Join(dir, "observations.json"), raw, 1<<20); e != nil {
		return e
	}
	ready, e := proof.Canonical(FinalizationReady{Kind: "local-ci-ready/v1", Identity: l.Identity, LedgerDigest: proof.Digest(ledgerData), ObservationsDigest: proof.Digest(raw)})
	if e != nil {
		return e
	}
	return publishImmutable(filepath.Join(dir, "finalization-ready.json"), ready, 4096)
}
func checkArtifacts(dir string, l *Ledger) error {
	if l.LogsDigest != "" {
		if e := verifyFile(filepath.Join(dir, "job.log"), l.LogsDigest, l.LogsBytes, observer.MaxLogs); e != nil {
			return e
		}
	}
	for _, a := range []struct {
		name, digest string
		limit        int64
	}{{"cleanup-evidence.json", l.EvidenceDigest, 900 << 10}, {"guard.json", l.GuardDigest, 16 << 10}} {
		if a.digest == "" {
			continue
		}
		raw, e := proof.ReadBounded(filepath.Join(dir, a.name), a.limit)
		if e != nil {
			return e
		}
		if proof.Digest(raw) != a.digest {
			return errors.New("evidence digest mismatch")
		}
	}
	return nil
}
func (c *Coordinator) LoadObservations(run string) (*Observations, error) {
	l, e := c.Load(run)
	if e != nil {
		return nil, e
	}
	dir, _ := c.runDir(run)
	ledgerData, e := proof.ReadBounded(filepath.Join(dir, "run.json"), 1<<20)
	if e != nil {
		return nil, e
	}
	data, e := proof.ReadBounded(filepath.Join(dir, "observations.json"), 1<<20)
	if e != nil {
		return nil, e
	}
	o, e := ValidateObservations(data, l, ledgerData)
	if e != nil {
		return nil, e
	}
	ready, e := proof.ReadBounded(filepath.Join(dir, "finalization-ready.json"), 4096)
	if e != nil {
		return nil, e
	}
	if e = ValidateFinalizationReady(ready, ledgerData, data); e != nil {
		return nil, e
	}
	if e = checkArtifacts(dir, l); e != nil {
		return nil, e
	}
	return o, nil
}
