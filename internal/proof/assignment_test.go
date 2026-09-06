package proof

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func testAssignment() Assignment {
	token := strings.Repeat("ab", 16)
	return Assignment{
		Kind:     "cleanup-assignment/v1",
		Identity: Identity{"github", "unit/repo", "123", 2, "job", strings.Repeat("a", 40)},
		Token:    token, Namespace: "proof-" + token,
		NamespaceUID: "resource-namespace-uid", ClusterUID: "cluster-uid",
	}
}

func assignmentNamespaces(a Assignment) (*core.Namespace, *core.Namespace) {
	return &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: "kube-system", UID: types.UID(a.ClusterUID)}},
		&core.Namespace{ObjectMeta: meta.ObjectMeta{Name: a.Namespace, UID: types.UID(a.NamespaceUID), Labels: map[string]string{Label: a.Token}}}
}

func assertAssignmentReads(t *testing.T, k *kubefake.Clientset, a Assignment) {
	t.Helper()
	for _, action := range k.Actions() {
		get, ok := action.(ktesting.GetAction)
		if !ok || action.GetVerb() != "get" || action.GetResource().Resource != "namespaces" ||
			(get.GetName() != "kube-system" && get.GetName() != a.Namespace) {
			t.Fatalf("unexpected Kubernetes action: %#v", action)
		}
	}
}

func TestBeginAssignedFreshSandboxAndState(t *testing.T) {
	a := testAssignment()
	system, ns := assignmentNamespaces(a)
	runner := &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: "runner-pool", UID: "runner-uid"}}
	k := kubefake.NewClientset(system, ns, runner)
	e := &Engine{K: k}
	base := filepath.Join(t.TempDir(), "new-parent")
	s, err := e.BeginAssigned(context.Background(), a, base)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Identity != a.Identity || s.Token != a.Token || s.Namespace != a.Namespace || s.NamespaceUID != a.NamespaceUID || s.ClusterUID != a.ClusterUID || !s.NamespaceScoped {
		t.Fatalf("assignment bindings changed: %+v", s)
	}
	ordinary, err := NewState(a.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != ordinary.Kind || !reflect.DeepEqual(s.Coverage, ordinary.Coverage) || !reflect.DeepEqual(s.Resources, ordinary.Resources) || s.InventoryComplete || s.CompletedAt != nil || s.CleanupAttempts != 0 {
		t.Fatalf("new-state semantics differ: %+v", s)
	}
	for _, name := range []string{"workspace", "credentials"} {
		entries, err := os.ReadDir(filepath.Join(s.Sandbox, name))
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s is not fresh: %v, %v", name, entries, err)
		}
	}
	marker, err := os.ReadFile(filepath.Join(s.Sandbox, ".proof-owner"))
	if err != nil || string(marker) != a.Token {
		t.Fatalf("marker: %q, %v", marker, err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err = Save(statePath, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(statePath)
	if err != nil || !reflect.DeepEqual(s, loaded) {
		t.Fatalf("state round trip: %+v, %v", loaded, err)
	}
	evidence, err := s.Evidence()
	if err != nil || evidence["verdict"] != "partial" {
		t.Fatalf("assignment inflated evidence: %v, %v", evidence, err)
	}
	if len(k.Actions()) != 2 {
		t.Fatalf("expected exactly two namespace reads, got %v", k.Actions())
	}
	assertAssignmentReads(t, k, a)
	got, err := k.Tracker().Get(core.SchemeGroupVersion.WithResource("namespaces"), "", a.Namespace)
	if err != nil || !reflect.DeepEqual(got, ns) {
		t.Fatalf("assigned namespace changed: %v, %v", got, err)
	}
	got, err = k.Tracker().Get(core.SchemeGroupVersion.WithResource("namespaces"), "", runner.Name)
	if err != nil || !reflect.DeepEqual(got, runner) {
		t.Fatalf("runner namespace changed: %v, %v", got, err)
	}

	keep := filepath.Join(s.Sandbox, "workspace", "keep")
	mustWrite(t, keep, "keep")
	if _, err = e.BeginAssigned(context.Background(), a, base); err == nil {
		t.Fatal("existing sandbox was adopted")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Fatalf("existing sandbox modified: %q, %v", data, err)
	}
	assertAssignmentReads(t, k, a)
}

func TestBeginAssignedMismatchHasNoFilesystemWrites(t *testing.T) {
	for _, mode := range []string{"cluster", "namespace-uid", "label", "missing-label", "missing-namespace", "missing-cluster", "terminating", "terminating-phase", "forbidden", "cancelled", "cancelled-after-read"} {
		t.Run(mode, func(t *testing.T) {
			a := testAssignment()
			system, ns := assignmentNamespaces(a)
			switch mode {
			case "cluster":
				system.UID = "other-cluster"
			case "namespace-uid":
				ns.UID = "recreated-namespace"
			case "label":
				ns.Labels[Label] = strings.Repeat("0", 32)
			case "missing-label":
				ns.Labels = nil
			case "terminating":
				now := meta.Now()
				ns.DeletionTimestamp = &now
			case "terminating-phase":
				ns.Status.Phase = core.NamespaceTerminating
			}
			objects := []runtime.Object{}
			if mode != "missing-cluster" {
				objects = append(objects, system)
			}
			if mode != "missing-namespace" {
				objects = append(objects, ns)
			}
			k := kubefake.NewClientset(objects...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "forbidden" || mode == "cancelled-after-read" {
				k.PrependReactor("get", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
					if mode == "forbidden" {
						return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, a.Namespace, errors.New("denied"))
					}
					if action.(ktesting.GetAction).GetName() == a.Namespace {
						cancel()
					}
					return false, nil, nil
				})
			}
			parent := t.TempDir()
			base := filepath.Join(parent, "must-not-exist")
			s, err := (&Engine{K: k}).BeginAssigned(ctx, a, base)
			if err == nil || s != nil {
				t.Fatalf("invalid assignment accepted: %+v, %v", s, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("filesystem changed before validation: %v, %v", entries, err)
			}
			assertAssignmentReads(t, k, a)
		})
	}
}

func TestBeginAssignedInvalidInputHasNoSideEffects(t *testing.T) {
	for _, mode := range []string{"kind", "identity", "token", "uppercase", "namespace", "namespace-uid", "cluster-uid", "uid-size", "base"} {
		t.Run(mode, func(t *testing.T) {
			a := testAssignment()
			parent := t.TempDir()
			base := filepath.Join(parent, "new")
			switch mode {
			case "kind":
				a.Kind = "cleanup-assignment/v2"
			case "identity":
				a.Identity.Attempt = 0
			case "token":
				a.Token = "../escape"
			case "uppercase":
				a.Token = strings.ToUpper(a.Token)
				a.Namespace = "proof-" + a.Token
			case "namespace":
				a.Namespace = "runner-pool"
			case "namespace-uid":
				a.NamespaceUID = ""
			case "cluster-uid":
				a.ClusterUID = " "
			case "uid-size":
				a.NamespaceUID = strings.Repeat("x", 256)
			case "base":
				base = ""
			}
			k := kubefake.NewClientset()
			if s, err := (&Engine{K: k}).BeginAssigned(context.Background(), a, base); err == nil || s != nil {
				t.Fatalf("invalid assignment accepted: %+v, %v", s, err)
			}
			if len(k.Actions()) != 0 {
				t.Fatal("invalid assignment reached Kubernetes")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid input changed filesystem: %v, %v", entries, err)
			}
		})
	}
}

func TestAssignmentStrictJSON(t *testing.T) {
	a := testAssignment()
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	path := filepath.Join(t.TempDir(), "assignment.json")
	mustWrite(t, path, valid)
	loaded, err := LoadAssignment(path)
	if err != nil || *loaded != a {
		t.Fatalf("valid assignment: %+v, %v", loaded, err)
	}
	for name, input := range map[string]string{
		"unknown":             strings.TrimSuffix(valid, "}") + `,"extra":1}`,
		"unknown-identity":    strings.Replace(valid, `"provider":`, `"extra":1,"provider":`, 1),
		"duplicate":           strings.Replace(valid, `"kind":`, `"kind":"cleanup-assignment/v1","kind":`, 1),
		"duplicate-identity":  strings.Replace(valid, `"provider":`, `"provider":"local","provider":`, 1),
		"escaped-duplicate":   strings.Replace(valid, `"token":`, `"\u0074oken":"ignored","token":`, 1),
		"case-alias":          strings.Replace(valid, `"token":`, `"TOKEN":`, 1),
		"identity-case-alias": strings.Replace(valid, `"provider":`, `"Provider":`, 1),
		"missing":             strings.Replace(valid, `"kind":"cleanup-assignment/v1",`, "", 1),
		"null-field":          strings.Replace(valid, `"run_attempt":2`, `"run_attempt":null`, 1),
		"null-identity":       strings.Replace(valid, `"identity":{`, `"identity":null,"unused":{`, 1),
		"null":                "null", "array": "[]", "trailing": valid + "{}",
		"wrong-type": strings.Replace(valid, `"run_attempt":2`, `"run_attempt":"2"`, 1),
		"oversized":  strings.Replace(valid, a.NamespaceUID, strings.Repeat("x", MaxAssignment), 1),
	} {
		t.Run(name, func(t *testing.T) {
			mustWrite(t, path, input)
			if _, err := LoadAssignment(path); err == nil {
				t.Fatal("invalid JSON accepted")
			}
			unchanged := a
			if err := json.Unmarshal([]byte(input), &unchanged); err == nil {
				t.Fatal("direct decoder accepted invalid JSON")
			}
			if unchanged != a {
				t.Fatal("failed decoding changed destination")
			}
		})
	}
}

func TestLoadAssignmentRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(dir, "assignment")
	data, err := json.Marshal(testAssignment())
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, regular, string(data))
	link := filepath.Join(dir, "link")
	if err = os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, link, dir} {
		if _, err := LoadAssignment(path); err == nil {
			t.Fatalf("special file accepted: %s", path)
		}
	}
}

type assignmentDiscovery struct {
	discovery.DiscoveryInterface
	calls int
}

func (d *assignmentDiscovery) ServerPreferredNamespacedResources() ([]*meta.APIResourceList, error) {
	d.calls++
	return []*meta.APIResourceList{{GroupVersion: "v1", APIResources: []meta.APIResource{{Name: "configmaps", Namespaced: true, Verbs: meta.Verbs{"list", "get"}}}}}, nil
}

func assignedCleanupEngine(t *testing.T) (*Engine, *State, *kubefake.Clientset, *dynamicfake.FakeDynamicClient, *assignmentDiscovery) {
	t.Helper()
	a := testAssignment()
	system, ns := assignmentNamespaces(a)
	k := kubefake.NewClientset(system, ns)
	d := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "configmaps"}:        "ConfigMapList",
		{Version: "v1", Resource: "persistentvolumes"}: "PersistentVolumeList",
	})
	disc := &assignmentDiscovery{}
	e := &Engine{K: k, D: d, Discovery: disc}
	s, err := e.BeginAssigned(context.Background(), a, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = []Ref{{Version: "v1", Resource: "configmaps", Namespace: s.Namespace, Name: "job-config", UID: "config-uid"}}
	k.ClearActions()
	return e, s, k, d, disc
}

func TestAssignedCleanupSurvivesNamespaceRBACRemoval(t *testing.T) {
	for _, mode := range []string{"delete", "already-terminating", "already-gone"} {
		t.Run(mode, func(t *testing.T) {
			e, s, k, d, disc := assignedCleanupEngine(t)
			nsResource := core.SchemeGroupVersion.WithResource("namespaces")
			terminating := mode == "already-terminating"
			if mode == "already-gone" {
				if err := k.Tracker().Delete(nsResource, "", s.Namespace); err != nil {
					t.Fatal(err)
				}
			}
			markTerminating := func() {
				obj, err := k.Tracker().Get(nsResource, "", s.Namespace)
				if err != nil {
					t.Fatal(err)
				}
				ns := obj.(*core.Namespace)
				now := meta.Now()
				ns.DeletionTimestamp = &now
				ns.Status.Phase = core.NamespaceTerminating
				if err = k.Tracker().Update(nsResource, ns, ""); err != nil {
					t.Fatal(err)
				}
				terminating = true
			}
			if terminating {
				markTerminating()
			}
			deleteRequested, polls := false, 0
			k.PrependReactor("delete", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
				options := action.(ktesting.DeleteAction).GetDeleteOptions()
				if options.Preconditions == nil || options.Preconditions.UID == nil || string(*options.Preconditions.UID) != s.NamespaceUID {
					t.Fatal("namespace delete lost UID precondition")
				}
				markTerminating()
				deleteRequested = true
				return true, nil, nil
			})
			k.PrependReactor("get", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.(ktesting.GetAction).GetName() == s.Namespace && deleteRequested {
					polls++
					if polls == 2 {
						if err := k.Tracker().Delete(nsResource, "", s.Namespace); err != nil {
							t.Fatal(err)
						}
					}
				}
				return false, nil, nil
			})
			d.PrependReactor("*", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetNamespace() != "" && (terminating || mode == "already-gone") {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: action.GetResource().Resource}, "job-config", errors.New("RoleBinding was garbage collected"))
				}
				return false, nil, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := e.Cleanup(ctx, s); err != nil {
				t.Fatal(err)
			}
			if s.Coverage["resources"].Status != "verified" || !strings.Contains(s.Coverage["resources"].Reason, "namespace GC") {
				t.Fatalf("incorrect scoped coverage: %+v", s.Coverage["resources"])
			}
			wantDiscovery := 0
			if mode == "delete" {
				wantDiscovery = 1
			}
			if disc.calls != wantDiscovery {
				t.Fatalf("discovery after RBAC loss: %d calls", disc.calls)
			}
			for _, action := range d.Actions() {
				if action.GetVerb() != "list" || mode != "delete" {
					t.Fatalf("scoped object queried during cleanup: %#v", action)
				}
			}
			if err := e.Inventory(ctx, s); err != nil {
				t.Fatalf("inventory after namespace gone: %v", err)
			}
			if disc.calls != wantDiscovery {
				t.Fatal("post-GC inventory rediscovered namespaced APIs")
			}
		})
	}
}

func TestAssignedCleanupDoesNotClaimNamespaceAbsenceEarly(t *testing.T) {
	for _, mode := range []string{"stuck", "replacement", "forbidden"} {
		t.Run(mode, func(t *testing.T) {
			e, s, k, _, _ := assignedCleanupEngine(t)
			deleted := false
			k.PrependReactor("delete", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) { deleted = true; return true, nil, nil })
			k.PrependReactor("get", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
				if deleted && action.(ktesting.GetAction).GetName() == s.Namespace {
					if mode == "forbidden" {
						return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, s.Namespace, errors.New("denied"))
					}
					_, ns := assignmentNamespaces(testAssignment())
					if mode == "replacement" {
						ns.UID = "replacement-uid"
					}
					return true, ns, nil
				}
				return false, nil, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			if err := e.Cleanup(ctx, s); err == nil {
				t.Fatal("namespace absence claimed without confirmation")
			}
			if s.Coverage["resources"].Status != "failed" {
				t.Fatal("failed resource evidence omitted")
			}
		})
	}
}

func TestAssignedCleanupPVsAreObservedButNeverDeleted(t *testing.T) {
	for _, mode := range []string{"absent", "residual", "replacement", "forbidden", "unlabelled", "late-volume"} {
		t.Run(mode, func(t *testing.T) {
			e, s, k, d, _ := assignedCleanupEngine(t)
			pv := &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: "job-pv", UID: "pv-uid", Labels: map[string]string{Label: s.Token}}}
			if mode != "late-volume" {
				s.Resources = append(s.Resources, Ref{Version: "v1", Resource: "persistentvolumes", Name: pv.Name, UID: string(pv.UID)})
			}
			if mode != "absent" && mode != "forbidden" && mode != "late-volume" {
				if mode == "unlabelled" {
					pv.Labels = nil
					pv.Spec.ClaimRef = &core.ObjectReference{Namespace: s.Namespace, Name: "claim", UID: "claim-uid"}
				}
				if err := k.Tracker().Add(pv); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "late-volume" {
				k.PrependReactor("delete", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
					if err := k.Tracker().Add(pv); err != nil {
						t.Fatal(err)
					}
					return false, nil, nil
				})
			}
			pvGets := 0
			d.PrependReactor("*", "persistentvolumes", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetVerb() != "get" {
					t.Fatalf("restricted runner tried PV mutation: %#v", action)
				}
				pvGets++
				if mode == "forbidden" {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumes"}, pv.Name, errors.New("denied"))
				}
				if mode == "absent" {
					return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "persistentvolumes"}, pv.Name)
				}
				copy := pv.DeepCopy()
				if mode == "replacement" {
					copy.UID = "other-pv"
				}
				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(copy)
				return true, &unstructured.Unstructured{Object: obj}, err
			})
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			err := e.RemoveResources(ctx, s)
			if (err == nil) != (mode == "absent") {
				t.Fatalf("unexpected PV cleanup result: %v", err)
			}
			if mode != "unlabelled" && pvGets == 0 {
				t.Fatal("PV absence was not individually checked")
			}
			for _, action := range k.Actions() {
				if action.GetResource().Resource == "persistentvolumes" && action.GetVerb() != "list" && action.GetVerb() != "get" {
					t.Fatalf("PV mutation: %#v", action)
				}
			}
		})
	}
}

func TestOrdinaryCleanupStillChecksNamespacedRefs(t *testing.T) {
	e, s, _, d, _ := assignedCleanupEngine(t)
	s.NamespaceScoped = false
	d.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "job-config", errors.New("per-ref read denied"))
	})
	if err := e.RemoveResources(context.Background(), s); !apierrors.IsForbidden(err) {
		t.Fatalf("ordinary context skipped per-ref checks: %v", err)
	}
	data, err := Canonical(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "namespace_scoped") {
		t.Fatal("optional field changed ordinary state JSON")
	}
}
