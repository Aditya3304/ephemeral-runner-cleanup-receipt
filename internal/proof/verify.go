package proof

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type VerifyOptions struct{ Artifact, Bundle, TrustRoot, ExpectedSHA256, Identity, Issuer, Cosign string }

// Verify validates the exact bytes, expected digest and certificate identity.
// Trust roots come from operator configuration, never from an uploaded bundle.
func Verify(ctx context.Context, o VerifyOptions) error {
	if len(o.ExpectedSHA256) != 64 || o.Identity == "" || o.Issuer == "" || o.TrustRoot == "" || o.Bundle == "" || o.Cosign == "" {
		return errors.New("artifact, bundle, operator trust root, expected SHA-256, exact issuer/identity and pinned Cosign are required")
	}
	artifact, err := ReadBounded(o.Artifact, MaxEvidence)
	if err != nil {
		return err
	}
	if Digest(artifact) != o.ExpectedSHA256 {
		return errors.New("artifact SHA-256 mismatch")
	}
	bundle, err := ReadBounded(o.Bundle, 2<<20)
	if err != nil {
		return err
	}
	trust, err := ReadBounded(o.TrustRoot, 1<<20)
	if err != nil {
		return err
	}
	binary, err := filepath.Abs(o.Cosign)
	if err != nil {
		return err
	}
	// Snapshot all inputs to close the validate/execute race. Cosign sees these
	// private files, not mutable job-provided paths. No shell evaluates arguments.
	dir, err := os.MkdirTemp("", "proof-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, data := range map[string][]byte{"artifact": artifact, "bundle": bundle, "trust": trust} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	command := exec.CommandContext(ctx, binary, "verify-blob", "--offline", "--trusted-root", filepath.Join(dir, "trust"), "--bundle", filepath.Join(dir, "bundle"), "--certificate-identity", o.Identity, "--certificate-oidc-issuer", o.Issuer, filepath.Join(dir, "artifact"))
	// Prevent ambient Sigstore configuration or proxy settings changing trust.
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir}
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 2048 {
			message = message[:2048]
		}
		return fmt.Errorf("signature verification failed: %w: %s", err, message)
	}
	return nil
}
