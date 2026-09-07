// Package api is the sole runtime writer of verified metadata.
package api

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

var ErrInvalid = errors.New("invalid_evidence")
var ErrUnavailable = errors.New("dependency_unavailable")
var ErrConflict = errors.New("identity_conflict")
var ErrNotFound = errors.New("not_found")

type Reader interface {
	Read(context.Context, archive.Ref) ([]byte, error)
}
type SignatureVerifier interface {
	Verify(context.Context, []byte, []byte) error
}
type Verifier struct {
	Archive    Reader
	Signature  SignatureVerifier
	Policy     finalizer.SignerPolicy
	Repository string
}
type Verified struct {
	Result  finalizer.Result
	Receipt finalizer.Receipt
}

func (v *Verifier) Verify(ctx context.Context, result finalizer.Result) (*Verified, error) {
	if result.Identity.Repository != v.Repository || !validIdentity(result.Identity) {
		return nil, ErrInvalid
	}
	b, err := proof.Canonical(result)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = finalizer.ParseResult(b); err != nil {
		return nil, ErrInvalid
	}
	raw, err := v.Archive.Read(ctx, result.Receipt)
	if err != nil {
		return nil, ErrUnavailable
	}
	if proof.Digest(raw) != result.Receipt.SHA256 || int64(len(raw)) != result.Receipt.SizeBytes {
		return nil, ErrInvalid
	}
	r, err := finalizer.ParseReceipt(raw)
	if err != nil || r.Identity != result.Identity || r.Finalizer != v.Policy {
		return nil, ErrInvalid
	}
	bundle, err := v.Archive.Read(ctx, result.Bundle)
	if err != nil {
		return nil, ErrUnavailable
	}
	if proof.Digest(bundle) != result.Bundle.SHA256 || int64(len(bundle)) != result.Bundle.SizeBytes {
		return nil, ErrInvalid
	}
	if err = v.Signature.Verify(ctx, raw, bundle); err != nil {
		return nil, ErrInvalid
	}
	files := map[string][]byte{}
	for name, ref := range r.Objects {
		data, e := v.Archive.Read(ctx, ref)
		if e != nil {
			return nil, ErrUnavailable
		}
		files[name] = data
	}
	for _, ref := range r.LogObjects {
		data, e := v.Archive.Read(ctx, ref)
		if e != nil {
			return nil, ErrUnavailable
		}
		files["job.log"] = data
	}
	if finalizer.ValidateArchived(r, files) != nil {
		return nil, ErrInvalid
	}
	return &Verified{Result: result, Receipt: *r}, nil
}

// Metadata never copies free-form reasons, shell commands, paths or raw logs.
// Stable machine identifiers are constrained, never transformed for dedup keys.
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)

func validIdentity(id proof.Identity) bool {
	return id.Validate() == nil && id.Provider == "local" && identifier.MatchString(id.Repository) && identifier.MatchString(id.Job) && regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id.Run)
}
func sanitizedCoverage(r finalizer.Receipt) map[string]proof.Observation {
	c := map[string]proof.Observation{}
	for k, o := range r.Coverage {
		o.Reason = strings.ReplaceAll(k, "_", " ") + ": " + o.Status + " (" + o.Observer + " observation). See signed archive for details."
		c[k] = o
	}
	return c
}
