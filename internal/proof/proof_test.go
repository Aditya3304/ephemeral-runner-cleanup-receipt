package proof

import (
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func localState(t *testing.T) *State {
	t.Helper()
	s, err := NewState(Identity{"local", "unit/repo", "run", 1, "job", strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	s.NamespaceUID = "namespace-uid"
	s.ClusterUID = "cluster-uid"
	if err = PrepareSandbox(t.TempDir(), s); err != nil {
		t.Fatal(err)
	}
	return s
}
func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWipeDoesNotFollowSymlinksOrHardlinks(t *testing.T) {
	s := localState(t)
	outside := filepath.Join(t.TempDir(), "sentinel")
	mustWrite(t, outside, "keep")
	work := filepath.Join(s.Sandbox, "workspace")
	if err := os.Mkdir(filepath.Join(work, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(work, ".hidden"), "secret")
	mustWrite(t, filepath.Join(work, "nested", "data"), "secret")
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(work, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(work, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := Wipe(context.Background(), s, "workspace"); err != nil {
		t.Fatal(err)
	}
	if err := Wipe(context.Background(), s, "workspace"); err != nil {
		t.Fatal("idempotence:", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep" {
		t.Fatal("sentinel changed", err)
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) != 0 {
		t.Fatal("residual paths", err)
	}
}
func TestReplacedWorkspaceAndOwnerMarkerAreRejected(t *testing.T) {
	for _, mode := range []string{"symlink", "directory", "marker"} {
		t.Run(mode, func(t *testing.T) {
			s := localState(t)
			work := filepath.Join(s.Sandbox, "workspace")
			outside := t.TempDir()
			mustWrite(t, filepath.Join(outside, "keep"), "keep")
			if mode == "marker" {
				mustWrite(t, filepath.Join(s.Sandbox, ".proof-owner"), strings.Repeat("0", 32))
			} else {
				if err := os.Rename(work, work+"-original"); err != nil {
					t.Fatal(err)
				}
				if mode == "symlink" {
					if err := os.Symlink(outside, work); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(work, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := Wipe(context.Background(), s, "workspace"); err == nil {
				t.Fatal("replacement accepted")
			}
			if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCancelledWipePreservesResidualEvidence(t *testing.T) {
	s := localState(t)
	mustWrite(t, filepath.Join(s.Sandbox, "workspace", "data"), "keep")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wipe(ctx, s, "workspace"); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Stat(filepath.Join(s.Sandbox, "workspace", "data")); err != nil {
		t.Fatal(err)
	}
}
func TestCanonicalUnsignedEvidenceAndStrictContext(t *testing.T) {
	s := localState(t)
	s.Record("resources", "verified", "namespace absent")
	s.Record("workspace", "verified", "empty")
	s.Record("credentials", "verified", "empty")
	evidence, err := s.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	if evidence["verdict"] != "partial" || evidence["signature_state"] != "unsigned" {
		t.Fatal("missing facts became success")
	}
	a, err := Canonical(map[string]any{"z": 1, "a": "x"})
	if err != nil || string(a) != `{"a":"x","z":1}` {
		t.Fatal("canonicalization", string(a), err)
	}
	path := filepath.Join(t.TempDir(), "context.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, strings.Replace(string(original), `"kind":"cleanup-context/v1"`, `"kind":"cleanup-context/v1","kind":"cleanup-context/v1"`, 1))
	if _, err := Load(path); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	mustWrite(t, path, strings.TrimSuffix(string(original), "}")+`,"unexpected":1}`)
	if _, err := Load(path); err == nil {
		t.Fatal("unknown key accepted")
	}
	s.Record("resources", "failed", "timeout")
	s.Record("resources", "verified", "later retry")
	evidence, _ = s.Evidence()
	if evidence["verdict"] != "fail" {
		t.Fatal("previous failure erased")
	}
}
func TestDirectoryIdentitySurvivesCanonicalization(t *testing.T) {
	s := localState(t)
	s.Directories["root"] = DirIdentity{18446744073709551614, 9007199254740993}
	path := filepath.Join(t.TempDir(), "context.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Directories["root"] != s.Directories["root"] {
		t.Fatal("directory identity rounded by canonical JSON")
	}
}

func TestBoundedReaderRejectsSpecialFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBounded(path, MaxEvidence); err == nil {
		t.Fatal("FIFO accepted")
	}
}

func TestControlFilesCannotRepopulateWorkspace(t *testing.T) {
	s := localState(t)
	if err := ControlPath(filepath.Join(s.Sandbox, "workspace", "context.json"), s); err == nil {
		t.Fatal("state allowed inside wipe target")
	}
	if err := ControlPath(filepath.Join(t.TempDir(), "context.json"), s); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Join(s.Sandbox, "workspace"), alias); err != nil {
		t.Fatal(err)
	}
	if err := ControlPath(filepath.Join(alias, "receipt.json"), s); err == nil {
		t.Fatal("symlink bypassed control path restriction")
	}
}

func fakeEngine(s *State, namespace *core.Namespace) *Engine {
	system := &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: "kube-system", UID: types.UID(s.ClusterUID)}}
	k := kubefake.NewClientset(system, namespace)
	d := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{})
	disc := k.Discovery().(*fake.FakeDiscovery)
	return &Engine{k, d, disc}
}
func TestNamespaceOwnershipAndClusterIdentity(t *testing.T) {
	s := localState(t)
	for _, mode := range []string{"uid", "label", "cluster"} {
		t.Run(mode, func(t *testing.T) {
			ns := &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: s.Namespace, UID: types.UID(s.NamespaceUID), Labels: map[string]string{Label: s.Token}}}
			if mode == "uid" {
				ns.UID = "different"
			}
			if mode == "label" {
				ns.Labels[Label] = "other"
			}
			e := fakeEngine(s, ns)
			copy := *s
			if mode == "cluster" {
				copy.ClusterUID = "different"
			}
			if _, err := e.Check(context.Background(), &copy); err == nil {
				t.Fatal("ownership mismatch accepted")
			}
			for _, a := range e.K.(*kubefake.Clientset).Actions() {
				if a.GetVerb() == "delete" {
					t.Fatal("ownership check deleted resource")
				}
			}
		})
	}
}
func TestRealOfflineSigstoreBundle(t *testing.T) {
	binary := os.Getenv("PROOF_COSIGN")
	if binary == "" {
		t.Skip("set PROOF_COSIGN to pinned binary; cli-check provides it")
	}
	base := "testdata/sigstore"
	artifact, err := os.ReadFile(filepath.Join(base, "artifact.txt"))
	if err != nil {
		t.Fatal(err)
	}
	o := VerifyOptions{Artifact: filepath.Join(base, "artifact.txt"), Bundle: filepath.Join(base, "bundle.json"), TrustRoot: filepath.Join(base, "trusted-root.json"), ExpectedSHA256: Digest(artifact), Identity: "https://github.com/sigstore-conformance/extremely-dangerous-public-oidc-beacon/.github/workflows/extremely-dangerous-oidc-beacon.yml@refs/heads/main", Issuer: "https://token.actions.githubusercontent.com", Cosign: binary}
	if err := Verify(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"digest", "identity", "issuer", "artifact", "signature", "trust"} {
		t.Run(mode, func(t *testing.T) {
			bad := o
			dir := t.TempDir()
			switch mode {
			case "digest":
				bad.ExpectedSHA256 = strings.Repeat("0", 64)
			case "identity":
				bad.Identity = "https://wrong.invalid"
			case "issuer":
				bad.Issuer = "https://wrong.invalid"
			case "artifact":
				bad.Artifact = filepath.Join(dir, "artifact")
				mustWrite(t, bad.Artifact, "tampered")
				bad.ExpectedSHA256 = Digest([]byte("tampered"))
			case "signature":
				data, err := os.ReadFile(o.Bundle)
				if err != nil {
					t.Fatal(err)
				}
				var bundle map[string]any
				if err = json.Unmarshal(data, &bundle); err != nil {
					t.Fatal(err)
				}
				bundle["messageSignature"].(map[string]any)["signature"] = "AAAA"
				data, err = json.Marshal(bundle)
				if err != nil {
					t.Fatal(err)
				}
				bad.Bundle = filepath.Join(dir, "bundle")
				mustWrite(t, bad.Bundle, string(data))
			case "trust":
				bad.TrustRoot = filepath.Join(dir, "root")
				mustWrite(t, bad.TrustRoot, `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1","tlogs":[],"certificateAuthorities":[],"ctlogs":[]}`)
			}
			if err := Verify(context.Background(), bad); err == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
}
