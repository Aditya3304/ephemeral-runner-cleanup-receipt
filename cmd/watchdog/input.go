package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

var errInvalidInputs = errors.New("coordinator input cannot satisfy the pinned finalizer contract")

// Reject incomplete historical formats without rewriting their frozen evidence.
// A filesystem read failure remains retryable; invalid bytes need operator review.
func canonicalLedger(data []byte, id proof.Identity) bool {
	var l coordinator.Ledger
	if json.Unmarshal(data, &l) != nil || l.Identity != id || l.Phase != "complete" {
		return false
	}
	want, err := proof.Canonical(l)
	return err == nil && bytes.Equal(data, want)
}

func validateInputs(dir string, id proof.Identity) error {
	ledger, err := proof.ReadBounded(filepath.Join(dir, "run.json"), 1<<20)
	if err != nil {
		return err
	}
	ready, err := proof.ReadBounded(filepath.Join(dir, "finalization-ready.json"), 16<<10)
	if err != nil {
		return err
	}
	observations, err := proof.ReadBounded(filepath.Join(dir, "observations.json"), 1<<20)
	if err != nil {
		return err
	}
	if !canonicalLedger(ledger, id) || coordinator.ValidateFinalizationReady(ready, ledger, observations) != nil {
		return errInvalidInputs
	}
	return nil
}
