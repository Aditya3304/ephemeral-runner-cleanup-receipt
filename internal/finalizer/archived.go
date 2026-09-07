package finalizer

import (
	"bytes"
	"errors"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

// ParseResult validates transport shape, not the caller's signature assertion.
func ParseResult(data []byte) (*Result, error) {
	var r Result
	if err := strictCanonical(data, 16<<10, &r); err != nil {
		return nil, err
	}
	if r.Kind != "cleanup-finalization-result/v1" || r.SignatureState != "verified" || r.Identity.Validate() != nil || r.Receipt.Validate() != nil || r.Bundle.Validate() != nil || r.Receipt.SizeBytes < 1 || r.Receipt.SizeBytes > MaxReceipt || r.Bundle.SizeBytes < 1 || r.Bundle.SizeBytes > MaxBundle {
		return nil, errors.New("invalid finalization result")
	}
	return &r, nil
}

// ValidateArchived re-derives coverage and bindings from exact archived bytes.
// The caller must independently authenticate the receipt before using this.
func ValidateArchived(r *Receipt, files map[string][]byte) error {
	var l coordinator.Ledger
	if err := strictCanonical(files["run.json"], MaxReceipt, &l); err != nil {
		return err
	}
	if l.Kind != "local-ci-run/v1" || l.Phase != "complete" || l.Token != r.Identity.Run || l.Identity != r.Identity {
		return errors.New("archived ledger identity mismatch")
	}
	refs := map[string]archive.Ref{}
	for k, v := range r.Objects {
		refs[k] = v
	}
	if len(r.LogObjects) == 1 {
		refs["job.log"] = r.LogObjects[0]
	}
	if len(refs) != len(files) {
		return errors.New("archived file set mismatch")
	}
	for k, ref := range refs {
		b, ok := files[k]
		if !ok || int64(len(b)) != ref.SizeBytes || proof.Digest(b) != ref.SHA256 {
			return errors.New("archived bytes mismatch")
		}
	}
	snaps := map[string]snapshot{}
	if _, ok := files["cleanup-evidence.json"]; !ok && r.EvidenceStatus == "invalid" {
		snaps["cleanup-evidence.json"] = snapshot{Status: "unreadable"}
	}
	expected, err := buildReceipt(&l, files, &retryState{Policy: r.Finalizer, FinalizedAt: r.FinalizedAt, Objects: refs, Snapshots: snaps})
	if err != nil {
		return err
	}
	a, _ := r.Canonical()
	b, _ := expected.Canonical()
	if !bytes.Equal(a, b) {
		return errors.New("receipt disagrees with archived observations")
	}
	return nil
}
