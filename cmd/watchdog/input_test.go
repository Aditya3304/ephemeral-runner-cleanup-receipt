package main

import (
	"encoding/json"
	"testing"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func TestHistoricalLedgerIsNotRewrittenForSigning(t *testing.T) {
	id := proof.Identity{Provider: "local", Repository: "Aditya3304/ephemeral-runner-cleanup-receipt", Run: "04f25cf4d6f18d6503592c780fdef959", Attempt: 1, Job: "isolation", Revision: "17136b09f14497acdd4c91c4d3f2b29be25ddd35"}
	l := coordinator.Ledger{Identity: id, Phase: "complete"}
	current, _ := proof.Canonical(l)
	if !canonicalLedger(current, id) {
		t.Fatal("current canonical ledger rejected")
	}
	var old map[string]any
	if err := json.Unmarshal(current, &old); err != nil {
		t.Fatal(err)
	}
	delete(old, "launch_attempted")
	legacy, _ := proof.Canonical(old)
	if canonicalLedger(legacy, id) {
		t.Fatal("missing required field accepted")
	}
	old["launch_attempted"] = false
	old["unexpected"] = "value"
	unknown, _ := proof.Canonical(old)
	if canonicalLedger(unknown, id) {
		t.Fatal("unknown field accepted")
	}
	other := id
	other.Job = "different"
	if canonicalLedger(current, other) {
		t.Fatal("changed identity accepted")
	}
}
