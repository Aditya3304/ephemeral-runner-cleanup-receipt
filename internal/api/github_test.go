package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubrun"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
)

// Deterministic boundary integration, NOT a substitute for live Cosign/MinIO.
// Use real Ed25519 here so API verification cannot accept a claimed signature.
type githubSigner struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
	signs   int
	offline bool
}

func (s *githubSigner) Sign(_ context.Context, b []byte) ([]byte, error) {
	if s.offline {
		return nil, errors.New("test signer offline")
	}
	s.signs++
	return ed25519.Sign(s.private, b), nil
}
func (s *githubSigner) Verify(_ context.Context, b, sig []byte) error {
	if !ed25519.Verify(s.public, b, sig) {
		return errors.New("test signature invalid")
	}
	return nil
}

type githubArchive map[string][]byte

func (m githubArchive) Put(_ context.Context, _ string, b []byte) (archive.Ref, error) {
	hash := proof.Digest(b)
	m[hash] = append([]byte{}, b...)
	return archive.Ref{Store: "local", Bucket: "receipts", Key: "archive/sha256/" + hash, VersionID: "test-version", SHA256: hash, SizeBytes: int64(len(b))}, nil
}
func (m githubArchive) Read(_ context.Context, r archive.Ref) ([]byte, error) {
	b, ok := m[r.SHA256]
	if !ok {
		return nil, errors.New("test archive unavailable")
	}
	return b, nil
}
func (m githubArchive) Verify(_ context.Context, r archive.Ref) error {
	b, ok := m[r.SHA256]
	if !ok || proof.Digest(b) != r.SHA256 || int64(len(b)) != r.SizeBytes {
		return errors.New("test archive mismatch")
	}
	return nil
}
func (m githubArchive) Protect(ctx context.Context, r archive.Ref) error { return m.Verify(ctx, r) }

func githubFixture(t *testing.T, scenario string, attempt ...int) (*finalizer.Engine, *Verifier, proof.Identity, githubArchive, *githubSigner) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	id := proof.Identity{Provider: "github", Repository: "owner/repo", Run: "12345", Attempt: 1, Job: "cleanup", Revision: strings.Repeat("a", 40)}
	if len(attempt) > 0 {
		id.Attempt = attempt[0]
	}
	l := &githubrun.Ledger{Kind: githubrun.Kind, Identity: id, WorkflowID: 7, WorkflowPath: ".github/workflows/cleanup.yml", JobID: 678, JobName: "cleanup", Conclusion: "success", StartedAt: now.Add(-10 * time.Minute), CompletedAt: now.Add(-3 * time.Minute), EvidenceStatus: "missing", LogsStatus: "collected"}
	files := map[string][]byte{"job.log": []byte("completed API job logs\n")}
	l.LogsDigest = proof.Digest(files["job.log"])
	l.LogsBytes = int64(len(files["job.log"]))
	if scenario != "missing-post" {
		s, err := proof.NewState(id)
		if err != nil {
			t.Fatal(err)
		}
		s.NamespaceUID = "job-namespace"
		s.Sandbox = "/data/" + s.Namespace
		s.Directories = map[string]proof.DirIdentity{"root": {Inode: 1}, "workspace": {Inode: 2}, "credentials": {Inode: 3}}
		s.ClusterUID = "job-cluster"
		s.StartedAt = l.StartedAt.Add(time.Minute)
		done := l.CompletedAt.Add(-time.Minute)
		s.CompletedAt = &done
		s.CleanupAttempts = 1
		s.InventoryComplete = true
		s.Resources = []proof.Ref{}
		for _, k := range []string{"workspace", "credentials", "resources"} {
			s.Coverage[k] = proof.Observation{Status: "verified", Observer: "job", Reason: "Job reports cleanup"}
		}
		if scenario == "cleanup-failure" {
			s.Coverage["resources"] = proof.Observation{Status: "failed", Observer: "job", Reason: "Resource cleanup timed out"}
		}
		if scenario == "command-failure" {
			l.Conclusion = "failure"
		}
		value, err := s.Evidence()
		if err != nil {
			t.Fatal(err)
		}
		evidence, _ := proof.Canonical(value)
		files["cleanup-evidence.json"] = evidence
		l.EvidenceDigest = proof.Digest(evidence)
		l.EvidenceStatus = "accepted-untrusted"
	}
	files["run.json"], _ = proof.Canonical(l)
	root := t.TempDir()
	input := filepath.Join(root, "input")
	if err := githubrun.Publish(input, l, files); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := &githubSigner{public: pub, private: priv}
	store := githubArchive{}
	policy := finalizer.SignerPolicy{Revision: strings.Repeat("b", 40), Issuer: "https://test.invalid", Identity: "test-only", TrustRootSHA256: strings.Repeat("c", 64), CosignSHA256: strings.Repeat("d", 64), SigningConfigSHA256: strings.Repeat("e", 64)}
	engine := &finalizer.Engine{InputDir: input, StateDir: filepath.Join(root, "state"), OutputDir: filepath.Join(root, "output"), Policy: policy, Archive: store, Signer: signer, Now: func() time.Time { return now }}
	return engine, &Verifier{Archive: store, Signature: signer, Policy: policy, Repository: id.Repository}, id, store, signer
}
func TestGitHubFinalizationThroughIndependentAPIVerification(t *testing.T) {
	for _, scenario := range []string{"normal", "command-failure", "cleanup-failure", "missing-post"} {
		t.Run(scenario, func(t *testing.T) {
			e, v, id, store, signer := githubFixture(t, scenario)
			ctx := context.Background()
			result, err := e.Run(ctx, githubrun.Directory(id))
			if err != nil {
				t.Fatal(err)
			}
			verified, err := v.Verify(ctx, *result)
			if err != nil {
				t.Fatal(err)
			}
			r := verified.Receipt
			want := "partial"
			if scenario == "cleanup-failure" {
				want = "fail"
			}
			if r.Verdict != want {
				t.Fatalf("got %s want %s", r.Verdict, want)
			}
			if r.Identity != id || r.Binding.JobUID != "678" || r.Binding.ClusterUID != "" || r.Coverage["runner_disposal"].Status != "unobservable" || r.Coverage["logs"].Observer != "trusted" {
				t.Fatal("GitHub binding/provenance lost")
			}
			if scenario == "missing-post" && r.EvidenceStatus != "missing" {
				t.Fatal("missing post obscured")
			}
			again, err := e.Run(ctx, githubrun.Directory(id))
			if err != nil || *again != *result || signer.signs != 1 {
				t.Fatalf("retry duplicated signing: %v", err)
			}
			bad := *result
			bad.Identity.Attempt++
			if _, err = v.Verify(ctx, bad); err == nil {
				t.Fatal("wrong attempt accepted")
			}
			bad = *result
			bad.Identity.Provider = "local"
			if _, err = v.Verify(ctx, bad); err == nil {
				t.Fatal("provider confusion accepted")
			}
			sig := store[result.Bundle.SHA256]
			store[result.Bundle.SHA256] = make([]byte, len(sig))
			if _, err = v.Verify(ctx, *result); err == nil {
				t.Fatal("forged signature accepted")
			}
			store[result.Bundle.SHA256] = sig
			logref := r.LogObjects[0]
			store[logref.SHA256] = []byte("tampered archived log")
			if _, err = v.Verify(ctx, *result); err == nil {
				t.Fatal("tampered logs accepted")
			}
			r.Coverage["resources"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "Job forged observer"}
			if r.Validate() == nil {
				t.Fatal("trusted cleanup claim accepted")
			}
		})
	}
}
func TestGitHubSigningOutageRetainsFrozenInputs(t *testing.T) {
	e, v, id, _, signer := githubFixture(t, "missing-post")
	ctx := context.Background()
	signer.offline = true
	if _, err := e.Run(ctx, githubrun.Directory(id)); err == nil {
		t.Fatal("offline signing succeeded")
	}
	signer.offline = false
	now := e.Now().Add(2 * time.Minute)
	e.Now = func() time.Time { return now }
	r, err := e.Run(ctx, githubrun.Directory(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(ctx, *r); err != nil {
		t.Fatal(err)
	}
	if signer.signs != 1 {
		t.Fatal("signed more than once")
	}
}
func TestGitHubRecoveryIdentityBoundary(t *testing.T) {
	id := proof.Identity{Provider: "github", Repository: "owner/repo", Run: "42", Attempt: 1, Job: "cleanup", Revision: strings.Repeat("a", 40)}
	report := watchdog.Report{Identity: id, Number: 1, Stage: "collect", Status: "running", LeaseOwner: watchdog.UUID()}
	expires := time.Now().UTC().Add(time.Minute)
	report.LeaseExpiresAt = &expires
	if !validIdentity(id) || !validRecovery(report) {
		t.Fatal("GitHub recovery rejected")
	}
	for _, run := range []string{"../42", "0042", "0", strings.Repeat("1", 32)} {
		bad := id
		bad.Run = run
		if validIdentity(bad) {
			t.Fatal("invalid GitHub ID accepted")
		}
	}
}
