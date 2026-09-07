package finalizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func testPolicy() SignerPolicy {
	return SignerPolicy{Revision: strings.Repeat("a", 40), Issuer: "https://issuer:8443", Identity: "finalizer@cleanup-receipt.local", TrustRootSHA256: strings.Repeat("b", 64), CosignSHA256: strings.Repeat("c", 64), SigningConfigSHA256: strings.Repeat("d", 64)}
}
func testIdentity() proof.Identity {
	return proof.Identity{Provider: "local", Repository: "owner/repo", Run: strings.Repeat("1", 32), Attempt: 1, Job: "job-1", Revision: strings.Repeat("e", 40)}
}
func testRef(b []byte) archive.Ref {
	return archive.Ref{Store: "local", Bucket: "receipts", Key: "archive/sha256/" + proof.Digest(b), VersionID: "version-1", SHA256: proof.Digest(b), SizeBytes: int64(len(b))}
}
func testReceipt() *Receipt {
	id := testIdentity()
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	ledger := testRef([]byte("ledger"))
	c := map[string]proof.Observation{}
	for _, k := range components {
		c[k] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "Missing independent observation"}
	}
	return &Receipt{Kind: "cleanup-receipt/v1", Identity: id, Binding: Binding{ClusterUID: "cluster", ResourceNamespace: "proof-" + id.Run, ResourceUID: "resource", RunnerNamespace: "proof-runner-" + id.Run, RunnerUID: "runner", JobUID: "job", PodUID: "pod", LedgerSHA256: ledger.SHA256}, Finalizer: testPolicy(), StartedAt: now, CompletedAt: now, FinalizedAt: now, EvidenceStatus: "missing", Coverage: c, Objects: map[string]archive.Ref{"run.json": ledger}, LogObjects: []archive.Ref{}, Verdict: "partial"}
}
func canonicalTest(t *testing.T, v any) []byte {
	t.Helper()
	b, err := proof.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestStrictReceiptSchema(t *testing.T) {
	good := canonicalTest(t, testReceipt())
	if _, err := ParseReceipt(good); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"duplicate":           append([]byte(`{"kind":"cleanup-receipt/v1",`), good[1:]...),
		"unknown":             append([]byte(`{"evil":1,`), good[1:]...),
		"trailing":            append(append([]byte{}, good...), []byte(`{}`)...),
		"whitespace":          append([]byte(" "), good...),
		"missing":             []byte(strings.Replace(string(good), `"log_objects":[],`, "", 1)),
		"null":                []byte(strings.Replace(string(good), `"log_objects":[]`, `"log_objects":null`, 1)),
		"case":                []byte(strings.Replace(string(good), `"kind":`, `"Kind":`, 1)),
		"oversize":            make([]byte, MaxReceipt+1),
		"fractional-attempt":  []byte(strings.Replace(string(good), `"run_attempt":1`, `"run_attempt":1.5`, 1)),
		"ref-content-type":    []byte(strings.Replace(string(good), `"bucket":"receipts"`, `"bucket":"receipts","content_type":"application/json"`, 1)),
		"ref-missing-version": []byte(strings.Replace(string(good), `,"version_id":"version-1"`, "", 1)),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReceipt(b); err == nil {
				t.Fatal("accepted invalid receipt")
			}
		})
	}
}
func TestReceiptSemantics(t *testing.T) {
	cases := map[string]func(*Receipt){
		"job-not-trusted": func(r *Receipt) {
			for k := range r.Coverage {
				r.Coverage[k] = proof.Observation{Status: "verified", Observer: "job", Reason: "job says yes"}
			}
			r.Verdict = "pass"
		},
		"unobserved-verified": func(r *Receipt) {
			r.Coverage["workspace"] = proof.Observation{Status: "verified", Observer: "none", Reason: "no"}
		},
		"wrong-verdict":     func(r *Receipt) { r.Verdict = "fail" },
		"missing-component": func(r *Receipt) { delete(r.Coverage, "logs") },
		"extra-component":   func(r *Receipt) { r.Coverage["extra"] = r.Coverage["logs"] },
		"long-reason":       func(r *Receipt) { o := r.Coverage["logs"]; o.Reason = strings.Repeat("a", 513); r.Coverage["logs"] = o },
		"url-store": func(r *Receipt) {
			v := r.Objects["run.json"]
			v.Store = "https://evil.invalid"
			r.Objects["run.json"] = v
		},
		"path-traversal": func(r *Receipt) { v := r.Objects["run.json"]; v.Key = "a/../secret"; r.Objects["run.json"] = v },
		"null-version":   func(r *Receipt) { v := r.Objects["run.json"]; v.VersionID = "null"; r.Objects["run.json"] = v },
		"negative-size":  func(r *Receipt) { v := r.Objects["run.json"]; v.SizeBytes = -1; r.Objects["run.json"] = v },
		"chronology":     func(r *Receipt) { r.FinalizedAt = r.StartedAt.Add(-time.Second) },
		"ledger-tamper":  func(r *Receipt) { r.Binding.LedgerSHA256 = strings.Repeat("f", 64) },
		"revision-short": func(r *Receipt) { r.Identity.Revision = "abc" },
		"unknown-object": func(r *Receipt) { r.Objects["https://evil"] = r.Objects["run.json"] },
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			r := testReceipt()
			modify(r)
			if _, err := ParseReceipt(canonicalTest(t, r)); err == nil {
				t.Fatal("accepted invalid semantics")
			}
		})
	}
	r := testReceipt()
	r.Coverage["workspace"] = proof.Observation{Status: "failed", Observer: "job", Reason: "Residual paths"}
	r.Verdict = "fail"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

// These deterministic boundary doubles exercise orchestration, not Sigstore.
// Real keyless interoperability belongs to the independently deployed stack.
type recordingSigner struct {
	signs, verifies      int
	failSign, failVerify bool
}

func (s *recordingSigner) Sign(_ context.Context, b []byte) ([]byte, error) {
	s.signs++
	if s.failSign {
		return nil, errors.New("injected signing outage")
	}
	return []byte("TEST-ONLY:" + proof.Digest(b)), nil
}
func (s *recordingSigner) Verify(_ context.Context, b, sig []byte) error {
	s.verifies++
	if s.failVerify || string(sig) != "TEST-ONLY:"+proof.Digest(b) {
		return errors.New("injected verification rejection")
	}
	return nil
}

type recordingArchive struct {
	objects     map[string][]byte
	expired     map[string]bool
	protects    int
	failProtect bool
	puts        int
	fail, wrong bool
	failName    string
}

func (a *recordingArchive) Protect(ctx context.Context, r archive.Ref) error {
	if a.failProtect {
		return errors.New("injected protection failure")
	}
	if err := a.Verify(ctx, r); err != nil {
		return err
	}
	a.protects++
	if a.expired != nil {
		a.expired[r.Key] = false
	}
	return nil
}

func TestProtectionFailureCannotPublish(t *testing.T) {
	for _, recovering := range []bool{false, true} {
		t.Run(fmt.Sprint(recovering), func(t *testing.T) {
			e, a, _, l, _ := setupEngine(t)
			if recovering {
				if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
					t.Fatal(err)
				}
				dir := stateDir(t, e)
				raw, _ := os.ReadFile(filepath.Join(dir, "state.json"))
				var state retryState
				if err := strictCanonical(raw, MaxReceipt, &state); err != nil {
					t.Fatal(err)
				}
				state.Attempts = 6
				state.Status = "running"
				state.Stage = "publish"
				if err := saveState(dir, &state); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(e.OutputDir, filepath.Base(dir), "result.json")); err != nil {
					t.Fatal(err)
				}
			}
			a.failProtect = true
			if _, err := e.Run(context.Background(), l.Identity.Run); err == nil {
				t.Fatal("published without admission protection")
			}
			markers, _ := filepath.Glob(filepath.Join(e.OutputDir, "*", "result.json"))
			if len(markers) != 0 {
				t.Fatal("published result marker despite protection failure")
			}
			a.failProtect = false
			advance(e)
			if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (a *recordingArchive) Put(_ context.Context, name string, b []byte) (archive.Ref, error) {
	a.puts++
	if a.fail || a.failName == name {
		return archive.Ref{}, errors.New("injected archive outage")
	}
	r := testRef(b)
	if a.wrong {
		r.SHA256 = strings.Repeat("0", 64)
	}
	a.objects[r.Key] = append([]byte{}, b...)
	return r, nil
}
func (a *recordingArchive) Verify(_ context.Context, r archive.Ref) error {
	b, ok := a.objects[r.Key]
	if a.fail || !ok || proof.Digest(b) != r.SHA256 || int64(len(b)) != r.SizeBytes {
		return errors.New("archive verification rejected")
	}
	return nil
}
func setupEngine(t *testing.T) (*Engine, *recordingArchive, *recordingSigner, *coordinator.Ledger, string) {
	t.Helper()
	root := t.TempDir()
	id := testIdentity()
	input := filepath.Join(root, "input", id.Run)
	if err := os.MkdirAll(input, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	l := &coordinator.Ledger{Kind: "local-ci-run/v1", Identity: id, Token: id.Run, ClusterUID: "cluster", ResourceNamespace: "proof-" + id.Run, ResourceUID: "resource", RunnerNamespace: "proof-runner-" + id.Run, RunnerUID: "runner", JobUID: "job", PodUID: "pod", Phase: "complete", CreatedAt: now, UpdatedAt: now.Add(time.Second), EvidenceStatus: "missing", LogsStatus: "missing", Errors: []string{}}
	writeTest(t, filepath.Join(input, "run.json"), canonicalTest(t, l))
	a := &recordingArchive{objects: map[string][]byte{}}
	s := &recordingSigner{}
	e := &Engine{InputDir: filepath.Dir(input), StateDir: filepath.Join(root, "state"), OutputDir: filepath.Join(root, "output"), Policy: testPolicy(), Archive: a, Signer: s, Now: func() time.Time { return now.Add(time.Hour) }}
	c := &coordinator.Coordinator{Root: e.InputDir}
	if _, err := c.Collect(context.Background(), id.Run); err != nil {
		t.Fatal(err)
	}
	return e, a, s, l, input
}

func TestCoordinatorPublicationMustCommitBeforeSnapshot(t *testing.T) {
	e, a, s, l, input := setupEngine(t)
	if err := os.Remove(filepath.Join(input, "finalization-ready.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(input, "observations.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), l.Identity.Run); !errors.Is(err, ErrCoordinatorNotReady) {
		t.Fatalf("uncommitted coordinator accepted: %v", err)
	}
	if s.signs != 0 || a.puts != 0 {
		t.Fatal("performed work before coordinator commit")
	}
	matches, _ := filepath.Glob(filepath.Join(e.StateDir, "*", "state.json"))
	if len(matches) != 0 {
		t.Fatal("froze incomplete coordinator input")
	}
	c := &coordinator.Coordinator{Root: e.InputDir}
	if _, err := c.Collect(context.Background(), l.Identity.Run); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
		t.Fatal(err)
	}
}

func TestSixthAttemptPublicationCrashReconciles(t *testing.T) {
	for _, stage := range []string{"archive_signed", "publish", "published"} {
		t.Run(stage, func(t *testing.T) {
			e, a, s, l, _ := setupEngine(t)
			result, err := e.Run(context.Background(), l.Identity.Run)
			if err != nil {
				t.Fatal(err)
			}
			dir := stateDir(t, e)
			b, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var state retryState
			if err = strictCanonical(b, MaxReceipt, &state); err != nil {
				t.Fatal(err)
			}
			state.Attempts = 6
			state.Status = "running"
			state.Stage = stage
			if err = saveState(dir, &state); err != nil {
				t.Fatal(err)
			}
			if stage != "published" {
				if err = os.Remove(filepath.Join(e.OutputDir, filepath.Base(dir), "result.json")); err != nil {
					t.Fatal(err)
				}
			}
			puts, signs := a.puts, s.signs
			again, err := e.Run(context.Background(), l.Identity.Run)
			if err != nil {
				t.Fatal(err)
			}
			if *again != *result || a.puts != puts || s.signs != signs {
				t.Fatal("recovery changed immutable result or charged external work")
			}
			b, _ = os.ReadFile(filepath.Join(dir, "state.json"))
			if err = strictCanonical(b, MaxReceipt, &state); err != nil {
				t.Fatal(err)
			}
			if state.Status != "complete" || state.Attempts != 6 {
				t.Fatal("sixth attempt did not reconcile")
			}
		})
	}
}
func writeTest(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func stateDir(t *testing.T, e *Engine) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(e.StateDir, "*", "state.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("state files %v %v", matches, err)
	}
	return filepath.Dir(matches[0])
}
func advance(e *Engine) { n := e.Now(); e.Now = func() time.Time { return n.Add(2 * time.Hour) } }
func TestMissingEvidenceSignedPartialAndReplay(t *testing.T) {
	e, a, s, l, _ := setupEngine(t)
	result, err := e.Run(context.Background(), l.Identity.Run)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ParseReceipt(a.objects[result.Receipt.Key])
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Verdict != "partial" || receipt.EvidenceStatus != "missing" || result.SignatureState != "verified" {
		t.Fatal("dishonest result")
	}
	puts := a.puts
	protects := a.protects
	again, err := e.Run(context.Background(), l.Identity.Run)
	if err != nil {
		t.Fatal(err)
	}
	if *result != *again || s.signs != 1 || a.puts != puts || a.protects != protects {
		t.Fatal("replay changed output or signed/uploaded again")
	}
}

func TestLongOfflineAdmissionRenewsEveryExactVersion(t *testing.T) {
	for _, mode := range []string{"sign_retry", "sixth_recovery"} {
		t.Run(mode, func(t *testing.T) {
			e, a, s, l, _ := setupEngine(t)
			if mode == "sign_retry" {
				s.failSign = true
				if _, err := e.Run(context.Background(), l.Identity.Run); err == nil {
					t.Fatal("expected signing failure")
				}
				s.failSign = false
			} else {
				if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
					t.Fatal(err)
				}
				dir := stateDir(t, e)
				raw, _ := os.ReadFile(filepath.Join(dir, "state.json"))
				var state retryState
				if err := strictCanonical(raw, MaxReceipt, &state); err != nil {
					t.Fatal(err)
				}
				state.Attempts = 6
				state.Status = "running"
				state.Stage = "publish"
				if err := saveState(dir, &state); err != nil {
					t.Fatal(err)
				}
			}
			a.expired = map[string]bool{}
			for key := range a.objects {
				a.expired[key] = true
			}
			dir := stateDir(t, e)
			raw, _ := os.ReadFile(filepath.Join(dir, "state.json"))
			var before retryState
			if err := strictCanonical(raw, MaxReceipt, &before); err != nil {
				t.Fatal(err)
			}
			now := e.Now()
			e.Now = func() time.Time { return now.Add(8 * 24 * time.Hour) }
			if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
				t.Fatal(err)
			}
			for key, expired := range a.expired {
				if expired {
					t.Fatalf("published expired saved version %s", key)
				}
			}
			raw, _ = os.ReadFile(filepath.Join(dir, "state.json"))
			var after retryState
			if err := strictCanonical(raw, MaxReceipt, &after); err != nil {
				t.Fatal(err)
			}
			for name, ref := range before.Objects {
				if after.Objects[name] != ref {
					t.Fatal("retention renewal reselected object version")
				}
			}
			protects := a.protects
			if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
				t.Fatal(err)
			}
			if a.protects != protects {
				t.Fatal("historical replay mutated retention")
			}
		})
	}
}
func TestIdentityUIDRevisionBinding(t *testing.T) {
	r := testReceipt()
	b := canonicalTest(t, r)
	s := &recordingSigner{}
	sig, _ := s.Sign(context.Background(), b)
	expected := Expected{r.Identity, r.Binding, r.Finalizer}
	if _, err := VerifyReceipt(context.Background(), b, sig, expected, s); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"run", "attempt", "job", "revision", "cluster", "resource", "runner", "pod", "jobuid", "policy"} {
		t.Run(field, func(t *testing.T) {
			x := expected
			switch field {
			case "run":
				x.Identity.Run = strings.Repeat("2", 32)
			case "attempt":
				x.Identity.Attempt++
			case "job":
				x.Identity.Job = "other"
			case "revision":
				x.Identity.Revision = strings.Repeat("f", 40)
			case "cluster":
				x.Binding.ClusterUID = "other"
			case "resource":
				x.Binding.ResourceUID = "other"
			case "runner":
				x.Binding.RunnerUID = "other"
			case "pod":
				x.Binding.PodUID = "other"
			case "jobuid":
				x.Binding.JobUID = "other"
			case "policy":
				x.Policy.Identity = "other"
			}
			if _, err := VerifyReceipt(context.Background(), b, sig, x, s); err == nil {
				t.Fatal("binding mismatch accepted")
			}
		})
	}
	tampered := append([]byte{}, b...)
	tampered[len(tampered)-2] = 'x'
	if _, err := VerifyReceipt(context.Background(), tampered, sig, expected, s); err == nil {
		t.Fatal("tamper accepted")
	}
}
func TestRetryArchiveSignAndRecovery(t *testing.T) {
	for _, stage := range []string{"archive", "sign", "verify", "signed_archive"} {
		t.Run(stage, func(t *testing.T) {
			e, a, s, l, _ := setupEngine(t)
			switch stage {
			case "archive":
				a.fail = true
			case "sign":
				s.failSign = true
			case "verify":
				s.failVerify = true
			case "signed_archive":
				a.failName = "receipt.bundle.json"
			}
			if _, err := e.Run(context.Background(), l.Identity.Run); err == nil {
				t.Fatal("outage accepted")
			}
			if _, err := os.Stat(filepath.Join(stateDir(t, e), "run.json")); err != nil {
				t.Fatal("snapshot lost")
			}
			if _, err := e.Run(context.Background(), l.Identity.Run); !errors.Is(err, ErrRetryNotDue) {
				t.Fatalf("backoff: %v", err)
			}
			a.fail = false
			a.failName = ""
			s.failSign = false
			s.failVerify = false
			advance(e)
			if _, err := e.Run(context.Background(), l.Identity.Run); err != nil {
				t.Fatal(err)
			}
			if (stage == "verify" || stage == "signed_archive") && s.signs != 1 {
				t.Fatal("durable bundle unnecessarily signed again")
			}
		})
	}
}
func TestRetryExhaustionRetainsState(t *testing.T) {
	e, a, _, l, _ := setupEngine(t)
	a.fail = true
	for i := 0; i < 6; i++ {
		if _, err := e.Run(context.Background(), l.Identity.Run); err == nil {
			t.Fatal("outage accepted")
		}
		advance(e)
	}
	if _, err := e.Run(context.Background(), l.Identity.Run); !errors.Is(err, ErrExhausted) {
		t.Fatal(err)
	}
	if a.puts != 6 {
		t.Fatal("exceeded attempt limit")
	}
	if _, err := os.Stat(filepath.Join(stateDir(t, e), "run.json")); err != nil {
		t.Fatal(err)
	}
}
func TestReplayConflictAndSnapshotTamper(t *testing.T) {
	for _, mode := range []string{"revision", "uid", "policy", "snapshot", "bundle", "archive"} {
		t.Run(mode, func(t *testing.T) {
			e, a, _, l, input := setupEngine(t)
			result, err := e.Run(context.Background(), l.Identity.Run)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "revision":
				l.Identity.Revision = strings.Repeat("f", 40)
				writeTest(t, filepath.Join(input, "run.json"), canonicalTest(t, l))
			case "uid":
				l.PodUID = "replaced"
				writeTest(t, filepath.Join(input, "run.json"), canonicalTest(t, l))
			case "policy":
				e.Policy.Revision = strings.Repeat("f", 40)
			case "snapshot":
				writeTest(t, filepath.Join(stateDir(t, e), "run.json"), []byte("tampered"))
			case "bundle":
				writeTest(t, filepath.Join(stateDir(t, e), "receipt.bundle.json"), []byte("tampered"))
			case "archive":
				a.objects[result.Receipt.Key] = []byte("tampered")
			}
			if _, err = e.Run(context.Background(), l.Identity.Run); err == nil {
				t.Fatal("tamper/replay accepted")
			}
		})
	}
}
func TestInvalidEvidenceAndWrongArchiveDigest(t *testing.T) {
	e, a, _, l, input := setupEngine(t)
	writeTest(t, filepath.Join(input, "cleanup-evidence.json"), []byte(`{"identity":"hostile","url":"https://evil.invalid"}`))
	result, err := e.Run(context.Background(), l.Identity.Run)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseReceipt(a.objects[result.Receipt.Key])
	if err != nil {
		t.Fatal(err)
	}
	if r.EvidenceStatus != "invalid" || r.Verdict != "partial" {
		t.Fatal("invalid input not represented honestly")
	}
	e, a, _, l, _ = setupEngine(t)
	a.wrong = true
	if _, err = e.Run(context.Background(), l.Identity.Run); err == nil {
		t.Fatal("wrong object digest accepted")
	}
}
func TestSymlinkAndOversizeInputs(t *testing.T) {
	for _, kind := range []string{"ledger-symlink", "evidence-symlink", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			e, a, _, l, input := setupEngine(t)
			switch kind {
			case "ledger-symlink":
				if err := os.Remove(filepath.Join(input, "run.json")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/zero", filepath.Join(input, "run.json")); err != nil {
					t.Fatal(err)
				}
			case "evidence-symlink":
				if err := os.Symlink("/dev/zero", filepath.Join(input, "cleanup-evidence.json")); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				writeTest(t, filepath.Join(input, "cleanup-evidence.json"), make([]byte, MaxReceipt+1))
			}
			result, err := e.Run(context.Background(), l.Identity.Run)
			if kind == "ledger-symlink" {
				if err == nil {
					t.Fatal("unsafe ledger accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, _ := ParseReceipt(a.objects[result.Receipt.Key])
			if r.Verdict == "pass" {
				t.Fatal("unsafe input passed")
			}
		})
	}
}
