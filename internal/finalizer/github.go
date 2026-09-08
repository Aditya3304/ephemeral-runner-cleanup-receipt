package finalizer

import (
	"encoding/json"
	"strconv"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubrun"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

// Adapter supplies only shared orchestration identity/timing. It does not
// synthesize local coordinator observations or Kubernetes allocation UIDs.
func githubLedger(g *githubrun.Ledger) *coordinator.Ledger {
	return &coordinator.Ledger{Kind: githubrun.Kind, Identity: g.Identity, CreatedAt: g.StartedAt, UpdatedAt: g.CompletedAt}
}

func buildGitHubReceipt(input map[string][]byte, s *retryState) (*Receipt, error) {
	g, err := githubrun.Parse(input["run.json"])
	if err != nil {
		return nil, err
	}
	if err = githubrun.ValidateFiles(g, input); err != nil {
		return nil, err
	}
	r := &Receipt{Kind: "cleanup-receipt/v1", Identity: g.Identity, Binding: Binding{LedgerSHA256: proof.Digest(input["run.json"])}, Finalizer: s.Policy, StartedAt: g.StartedAt, CompletedAt: g.CompletedAt, FinalizedAt: s.FinalizedAt, EvidenceStatus: g.EvidenceStatus, Coverage: map[string]proof.Observation{}, Objects: map[string]archive.Ref{}, LogObjects: []archive.Ref{}}
	if g.JobID > 0 {
		r.Binding.JobUID = strconv.FormatInt(g.JobID, 10)
	}
	for _, k := range components {
		r.Coverage[k] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "No independent runner cleanup observation available"}
	}
	if g.EvidenceStatus == "accepted-untrusted" {
		var p struct {
			Coverage map[string]proof.Observation `json:"coverage"`
		}
		if err = json.Unmarshal(input["cleanup-evidence.json"], &p); err != nil {
			return nil, err
		}
		for _, k := range []string{"workspace", "credentials", "resources"} {
			o := p.Coverage[k]
			o.Reason = boundedReason(o.Reason)
			r.Coverage[k] = o
		}
	} else {
		r.Coverage["workspace"] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "Preliminary cleanup evidence is " + g.EvidenceStatus}
	}
	for name, ref := range s.Objects {
		switch name {
		case "run.json", "cleanup-evidence.json":
			r.Objects[name] = ref
		case "job.log":
			r.LogObjects = append(r.LogObjects, ref)
		}
	}
	if g.LogsStatus == "collected" {
		r.Coverage["logs"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "Completed job logs downloaded from the GitHub API and bound to immutable archive bytes"}
	}
	r.Coverage["runner_disposal"] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "GitHub job completion does not independently prove runner VM destruction"}
	r.Verdict = Verdict(r.Coverage)
	return r, r.Validate()
}
