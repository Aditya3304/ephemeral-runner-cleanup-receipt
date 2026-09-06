package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func gateFixture() (*Coordinator, *fake.Clientset, *Ledger, *core.Pod) {
	token := strings.Repeat("a", 32)
	l := &Ledger{Token: token, RunnerNamespace: "proof-runner-" + token, PodName: "job-canary", PodUID: "796b063d-c008-4432-a6aa-891b186535d2"}
	pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: l.PodName, Namespace: l.RunnerNamespace, UID: types.UID(l.PodUID), Labels: labels(l)}, Spec: core.PodSpec{NodeName: gateNode}, Status: core.PodStatus{PodIP: "10.244.0.20", InitContainerStatuses: []core.ContainerStatus{{Name: "network-gate", State: core.ContainerState{Running: &core.ContainerStateRunning{}}}}}}
	k := fake.NewClientset(
		&core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "runner-gate", Namespace: l.RunnerNamespace, UID: "gate-uid", ResourceVersion: "7", Labels: labels(l)}, Data: map[string]string{"ready": "false"}},
		&core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "runner-gate-request", Namespace: l.RunnerNamespace, UID: "request-uid", ResourceVersion: "9", Labels: labels(l)}, Data: map[string]string{"nonce": strings.Repeat("c", 64), "pod_uid": l.PodUID}},
		&core.Node{ObjectMeta: meta.ObjectMeta{Name: gateNode}, Status: core.NodeStatus{Addresses: []core.NodeAddress{{Type: core.NodeInternalIP, Address: "172.19.0.2"}}}},
		&core.Service{ObjectMeta: meta.ObjectMeta{Name: "kubernetes", Namespace: "default"}, Spec: core.ServiceSpec{ClusterIP: "10.96.0.1"}},
	)
	return &Coordinator{K: k}, k, l, pod
}

func gateListing(l *Ledger, state string) []byte {
	data, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"id": strings.Repeat("b", 64), "state": state, "metadata": map[string]any{"namespace": l.RunnerNamespace, "name": l.PodName, "uid": l.PodUID}}}})
	return data
}

func assertGateClosed(t *testing.T, k *fake.Clientset, l *Ledger) {
	t.Helper()
	cm, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	if err != nil || cm.Data["ready"] != "false" {
		t.Fatalf("gate released on failure: %v %v", cm, err)
	}
	for _, a := range k.Actions() {
		if a.GetVerb() == "patch" {
			t.Fatal("attempted gate patch before verification")
		}
	}
}

func TestNetworkGateHelperSuccessBindsUIDWithCAS(t *testing.T) {
	c, k, l, pod := gateFixture()
	var calls [][]string
	err := c.ensureGateWith(context.Background(), l, pod, func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if len(calls) == 1 {
			return gateListing(l, "SANDBOX_READY"), nil
		}
		assertGateClosed(t, k, l)
		return []byte("verified"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantList := []string{"exec", gateNode, "/usr/local/bin/crictl", "--runtime-endpoint", "unix:///run/containerd/containerd.sock", "pods", "--namespace", l.RunnerNamespace, "--name", l.PodName, "-o", "json"}
	wantHelper := []string{"exec", gateNode, "/usr/local/bin/proof-netgate", "--sandbox", strings.Repeat("b", 64), "--pod-uid", l.PodUID, "--namespace", l.RunnerNamespace, "--api-ip", "10.96.0.1", "--endpoint-ip", "172.19.0.2"}
	if !reflect.DeepEqual(calls, [][]string{wantList, wantHelper}) {
		t.Fatalf("unexpected privileged boundary: %v", calls)
	}
	cm, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	if err != nil || cm.Data["ready"] != "true" || cm.Data["pod_uid"] != l.PodUID || cm.Data["nonce"] != strings.Repeat("c", 64) {
		t.Fatal(cm, err)
	}
	var patch []map[string]any
	for _, action := range k.Actions() {
		if p, ok := action.(ktesting.PatchAction); ok {
			if err = json.Unmarshal(p.GetPatch(), &patch); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(patch) != 3 || patch[0]["op"] != "test" || patch[0]["path"] != "/metadata/uid" || patch[0]["value"] != "gate-uid" || patch[1]["path"] != "/metadata/resourceVersion" || patch[1]["value"] != "7" {
		t.Fatalf("missing compare-and-swap: %v", patch)
	}
}

func TestNetworkGateFailureOrPendingNeverReleases(t *testing.T) {
	for _, kind := range []string{"listing error", "malformed JSON", "no sandbox", "sandbox pending", "helper error", "helper pending"} {
		t.Run(kind, func(t *testing.T) {
			c, k, l, pod := gateFixture()
			calls := 0
			err := c.ensureGateWith(context.Background(), l, pod, func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					switch kind {
					case "listing error":
						return nil, errors.New("unavailable")
					case "malformed JSON":
						return []byte("{"), nil
					case "no sandbox":
						return []byte(`{"items":[]}`), nil
					case "sandbox pending":
						return gateListing(l, "SANDBOX_NOTREADY"), nil
					}
					return gateListing(l, "SANDBOX_READY"), nil
				}
				if kind == "helper pending" {
					return nil, exec.Command("/bin/sh", "-c", "exit 75").Run()
				}
				return nil, errors.New("helper verification failed")
			})
			pending := kind == "no sandbox" || kind == "sandbox pending" || kind == "helper pending"
			if (err == nil) != pending {
				t.Fatalf("wrong error classification: %v", err)
			}
			assertGateClosed(t, k, l)
		})
	}
}

func TestNetworkGateOnlyRunsWhileInitIsWaiting(t *testing.T) {
	for _, kind := range []string{"unscheduled", "no IP", "init pending", "wrong node", "host network", "workload running"} {
		t.Run(kind, func(t *testing.T) {
			c, k, l, pod := gateFixture()
			switch kind {
			case "unscheduled":
				pod.Spec.NodeName = ""
			case "no IP":
				pod.Status.PodIP = ""
			case "init pending":
				pod.Status.InitContainerStatuses = nil
			case "wrong node":
				pod.Spec.NodeName = "foreign-control-plane"
			case "host network":
				pod.Spec.HostNetwork = true
			case "workload running":
				pod.Status.ContainerStatuses = []core.ContainerStatus{{Name: "guard", State: core.ContainerState{Running: &core.ContainerStateRunning{}}}}
			}
			err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) { t.Fatal("unexpected Docker call"); return nil, nil })
			pending := kind == "unscheduled" || kind == "no IP" || kind == "init pending"
			if (err == nil) != pending {
				t.Fatalf("wrong error classification: %v", err)
			}
			assertGateClosed(t, k, l)
		})
	}
}

func TestNetworkGateReadyMarkerRequiresIdentity(t *testing.T) {
	for _, kind := range []string{"bound", "unbound", "other UID", "foreign label", "invalid ready"} {
		t.Run(kind, func(t *testing.T) {
			c, k, l, pod := gateFixture()
			cm, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
			cm.Data = map[string]string{"ready": "true", "pod_uid": l.PodUID, "nonce": strings.Repeat("c", 64)}
			switch kind {
			case "unbound":
				delete(cm.Data, "pod_uid")
			case "other UID":
				cm.Data["pod_uid"] = "another-pod"
			case "foreign label":
				cm.Labels["app.kubernetes.io/managed-by"] = "other"
			case "invalid ready":
				cm.Data["ready"] = "TRUE"
			}
			if _, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), cm, meta.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
				t.Fatal("ready or invalid marker must not execute helper")
				return nil, nil
			})
			if (err == nil) != (kind == "bound") {
				t.Fatalf("wrong ready-marker handling: %v", err)
			}
		})
	}
}

func TestNetworkGateConcurrentConfigMapReplacementNotReleased(t *testing.T) {
	c, k, l, pod := gateFixture()
	calls := 0
	err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return gateListing(l, "SANDBOX_READY"), nil
		}
		cm, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
		cm.UID = "replacement"
		cm.ResourceVersion = "8"
		_, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), cm, meta.UpdateOptions{})
		return nil, err
	})
	if err == nil {
		t.Fatal("released a replaced gate")
	}
	cm, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	if cm.Data["ready"] != "false" {
		t.Fatal("replacement changed")
	}
}

func TestNetworkGateSandboxSelectionRejectsAmbiguity(t *testing.T) {
	_, _, l, _ := gateFixture()
	good := string(gateListing(l, "SANDBOX_READY"))
	for _, kind := range []string{"exact", "wrong UID", "invalid ID", "duplicate", "regex-only name"} {
		t.Run(kind, func(t *testing.T) {
			raw := good
			switch kind {
			case "wrong UID":
				raw = strings.Replace(raw, l.PodUID, "foreign", 1)
			case "invalid ID":
				raw = strings.Replace(raw, strings.Repeat("b", 64), "short", 1)
			case "duplicate":
				var m map[string][]json.RawMessage
				_ = json.Unmarshal([]byte(raw), &m)
				m["items"] = append(m["items"], m["items"][0])
				b, _ := json.Marshal(m)
				raw = string(b)
			case "regex-only name":
				raw = strings.Replace(raw, l.PodName, l.PodName+"-other", 1)
			}
			id, err := selectGateSandbox([]byte(raw), l.RunnerNamespace, l.PodName, l.PodUID)
			if kind == "exact" {
				if err != nil || id != strings.Repeat("b", 64) {
					t.Fatal(id, err)
				}
			} else if kind == "regex-only name" {
				if err != nil || id != "" {
					t.Fatal(id, err)
				}
			} else if err == nil {
				t.Fatal("unsafe sandbox selected")
			}
		})
	}
}

func TestNetworkGateOldReadyNewNonceReverifiesSandbox(t *testing.T) {
	c, k, l, pod := gateFixture()
	cm, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	cm.Data = map[string]string{"ready": "true", "pod_uid": l.PodUID, "nonce": strings.Repeat("d", 64)}
	if _, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), cm, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return gateListing(l, "SANDBOX_READY"), nil
		}
		before, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
		if before.Data["nonce"] != strings.Repeat("d", 64) {
			t.Fatal("nonce released before helper success")
		}
		return nil, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("old ready marker skipped sandbox verification: calls=%d err=%v", calls, err)
	}
	cm, _ = k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	if cm.Data["ready"] != "true" || cm.Data["pod_uid"] != l.PodUID || cm.Data["nonce"] != strings.Repeat("c", 64) {
		t.Fatalf("new init not bound: %v", cm.Data)
	}
}

func TestNetworkGateRequestMustBeOwnedAndInitialized(t *testing.T) {
	for _, kind := range []string{"missing CM", "empty data", "empty nonce", "short nonce", "uppercase nonce", "wrong pod UID", "missing pod UID", "foreign label", "missing UID", "missing version", "terminating"} {
		t.Run(kind, func(t *testing.T) {
			c, k, l, pod := gateFixture()
			request, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate-request", meta.GetOptions{})
			switch kind {
			case "empty data":
				request.Data = map[string]string{}
			case "empty nonce":
				delete(request.Data, "nonce")
			case "short nonce":
				request.Data["nonce"] = "bad"
			case "uppercase nonce":
				request.Data["nonce"] = strings.Repeat("A", 64)
			case "wrong pod UID":
				request.Data["pod_uid"] = "other"
			case "missing pod UID":
				delete(request.Data, "pod_uid")
			case "foreign label":
				request.Labels["app.kubernetes.io/managed-by"] = "foreign"
			case "missing UID":
				request.UID = ""
			case "missing version":
				request.ResourceVersion = ""
			case "terminating":
				now := meta.Now()
				request.DeletionTimestamp = &now
			}
			if kind == "missing CM" {
				if err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Delete(context.Background(), request.Name, meta.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), request, meta.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
				t.Fatal("uninitialized/untrusted request executed helper")
				return nil, nil
			})
			pending := kind == "missing CM" || kind == "empty data" || kind == "empty nonce"
			if (err == nil) != pending {
				t.Fatalf("wrong request classification: %v", err)
			}
			assertGateClosed(t, k, l)
		})
	}
}

func TestNetworkGateChangedRequestDuringHelperNeverReleases(t *testing.T) {
	for _, kind := range []string{"nonce", "resourceVersion", "UID", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			c, k, l, pod := gateFixture()
			calls := 0
			err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					return gateListing(l, "SANDBOX_READY"), nil
				}
				request, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate-request", meta.GetOptions{})
				switch kind {
				case "nonce":
					request.Data["nonce"] = strings.Repeat("d", 64)
				case "resourceVersion":
					request.ResourceVersion = "10"
				case "UID":
					request.UID = "replaced-request"
				case "deleted":
					return nil, k.CoreV1().ConfigMaps(l.RunnerNamespace).Delete(context.Background(), request.Name, meta.DeleteOptions{})
				}
				_, err := k.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), request, meta.UpdateOptions{})
				return nil, err
			})
			if err != nil || calls != 2 {
				t.Fatal(calls, err)
			}
			assertGateClosed(t, k, l)
			if kind == "nonce" {
				// A later poll must verify again, then acknowledge the new invocation.
				calls = 0
				err = c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
					calls++
					if calls == 1 {
						return gateListing(l, "SANDBOX_READY"), nil
					}
					return nil, nil
				})
				if err != nil || calls != 2 {
					t.Fatal(calls, err)
				}
				cm, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
				if cm.Data["nonce"] != strings.Repeat("d", 64) || cm.Data["ready"] != "true" {
					t.Fatal(cm.Data)
				}
			}
		})
	}
}

func TestNetworkGateRequestChangeAfterFinalReadCannotAuthorizeNewNonce(t *testing.T) {
	c, k, l, pod := gateFixture()
	k.PrependReactor("patch", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.PatchAction).GetName() != "runner-gate" {
			return false, nil, nil
		}
		gvr := core.SchemeGroupVersion.WithResource("configmaps")
		obj, err := k.Tracker().Get(gvr, l.RunnerNamespace, "runner-gate-request")
		if err != nil {
			return true, nil, err
		}
		request := obj.(*core.ConfigMap).DeepCopy()
		request.ResourceVersion = "10"
		request.Data["nonce"] = strings.Repeat("d", 64)
		if err = k.Tracker().Update(gvr, request, l.RunnerNamespace); err != nil {
			return true, nil, err
		}
		return false, nil, nil
	})
	calls := 0
	err := c.ensureGateWith(context.Background(), l, pod, func(context.Context, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return gateListing(l, "SANDBOX_READY"), nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate", meta.GetOptions{})
	request, _ := k.CoreV1().ConfigMaps(l.RunnerNamespace).Get(context.Background(), "runner-gate-request", meta.GetOptions{})
	if gate.Data["nonce"] != strings.Repeat("c", 64) || gate.Data["nonce"] == request.Data["nonce"] {
		t.Fatalf("new init could accept stale acknowledgement: gate=%v request=%v", gate.Data, request.Data)
	}
}
