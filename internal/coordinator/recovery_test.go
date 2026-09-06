package coordinator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	ktesting "k8s.io/client-go/testing"
)

type contextCheckingClient struct {
	kubernetes.Interface
	check func(context.Context, string)
}

func (k contextCheckingClient) CoreV1() coreclient.CoreV1Interface {
	return contextCheckingCore{CoreV1Interface: k.Interface.CoreV1(), check: k.check}
}

type contextCheckingCore struct {
	coreclient.CoreV1Interface
	check func(context.Context, string)
}

func (k contextCheckingCore) Namespaces() coreclient.NamespaceInterface {
	return contextCheckingNamespaces{NamespaceInterface: k.CoreV1Interface.Namespaces(), check: k.check}
}

type contextCheckingNamespaces struct {
	coreclient.NamespaceInterface
	check func(context.Context, string)
}

func (k contextCheckingNamespaces) Get(ctx context.Context, name string, options meta.GetOptions) (*core.Namespace, error) {
	k.check(ctx, name)
	return k.NamespaceInterface.Get(ctx, name, options)
}

func recoveryFixture(t *testing.T, l *Ledger, extra ...runtime.Object) (*Coordinator, *fake.Clientset) {
	t.Helper()
	objects := []runtime.Object{
		&core.Namespace{ObjectMeta: meta.ObjectMeta{Name: "kube-system", UID: types.UID(l.ClusterUID)}},
		&core.Namespace{ObjectMeta: meta.ObjectMeta{Name: l.ResourceNamespace, UID: "resource-uid", Labels: labels(l)}},
		&core.Namespace{ObjectMeta: meta.ObjectMeta{Name: l.RunnerNamespace, UID: "runner-uid", Labels: labels(l)}},
		&rbac.ClusterRole{ObjectMeta: meta.ObjectMeta{Name: "proof-guard-" + l.Token, UID: "role-uid", Labels: labels(l)}},
		&rbac.ClusterRoleBinding{ObjectMeta: meta.ObjectMeta{Name: "proof-guard-" + l.Token, UID: "binding-uid", Labels: labels(l)}},
	}
	k := fake.NewClientset(append(objects, extra...)...)
	c := &Coordinator{K: k, Root: t.TempDir(), Out: io.Discard}
	if err := c.publish(l); err != nil {
		t.Fatal(err)
	}
	return c, k
}

func assertNoLaunch(t *testing.T, k *fake.Clientset) {
	t.Helper()
	for _, a := range k.Actions() {
		if a.GetVerb() == "create" {
			t.Fatalf("recovery created %s", a.GetResource().Resource)
		}
	}
}

func TestRecoveryMissingLaunchNeverReplays(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ambiguous", true: "cancelled-prelaunch"}[cancelled], func(t *testing.T) {
			l := ledgerFixture()
			l.Phase = "launching"
			l.LaunchAttempted = !cancelled
			// Simulate API creates succeeding immediately before their UID saves.
			l.ResourceUID, l.RunnerUID, l.RoleUID, l.BindingUID = "", "", "", ""
			c, k := recoveryFixture(t, l)
			if cancelled {
				if err := c.RequestCancel(l.Identity.Run); err != nil {
					t.Fatal(err)
				}
			}
			got, err := c.Collect(context.Background(), l.Identity.Run)
			if err != nil {
				t.Fatal(err)
			}
			if got.Phase != "complete" || !got.RunnerAbsent || !got.ResourceAbsent || got.CancelRequested != cancelled {
				t.Fatalf("recovery incomplete: %+v", got)
			}
			if !cancelled && !strings.Contains(strings.Join(got.Errors, " "), "runner-launch-ambiguous") {
				t.Fatal("ambiguous execution not recorded")
			}
			assertNoLaunch(t, k)
			for _, a := range k.Actions() {
				if a.GetVerb() == "delete" && a.(ktesting.DeleteAction).GetDeleteOptions().Preconditions == nil {
					t.Fatal("disposal lacked UID precondition")
				}
			}
			loaded, err := c.Load(l.Identity.Run)
			if err != nil || loaded.RoleUID != "role-uid" || loaded.BindingUID != "binding-uid" || loaded.ResourceUID != "resource-uid" || loaded.RunnerUID != "runner-uid" {
				t.Fatalf("adopted ownership not durable: %+v, %v", loaded, err)
			}
		})
	}
}

func TestRecoveryRejectsForeignPartialOwnership(t *testing.T) {
	l := ledgerFixture()
	l.Phase, l.LaunchAttempted = "launching", true
	l.ResourceUID = ""
	c, k := recoveryFixture(t, l)
	ns, _ := k.CoreV1().Namespaces().Get(context.Background(), l.ResourceNamespace, meta.GetOptions{})
	delete(ns.Labels, "app.kubernetes.io/managed-by")
	_, _ = k.CoreV1().Namespaces().Update(context.Background(), ns, meta.UpdateOptions{})
	got, err := c.Collect(context.Background(), l.Identity.Run)
	if err == nil || got.Phase != "disposing" || got.ResourceAbsent || !got.RunnerAbsent {
		t.Fatalf("foreign partial adoption: %+v, %v", got, err)
	}
	if _, err := k.CoreV1().Namespaces().Get(context.Background(), l.ResourceNamespace, meta.GetOptions{}); err != nil {
		t.Fatal("foreign namespace removed")
	}
	assertNoLaunch(t, k)
}

func TestTransientCollectionRetainsSources(t *testing.T) {
	for _, failure := range []string{"stage-api", "stage-filesystem", "logs-api"} {
		t.Run(failure, func(t *testing.T) {
			l := ledgerFixture()
			l.Phase, l.LaunchAttempted = "collecting", true
			l.EvidenceStatus, l.LogsStatus = "missing", "missing"
			stage := &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "guard-stage", Namespace: l.RunnerNamespace, UID: types.UID(l.StageUID)}, Data: map[string]string{}}
			if failure == "stage-filesystem" {
				stage.Data["guard.json"] = "{}"
			}
			if failure == "logs-api" {
				l.PodUID, l.PodName = "pod-uid", "runner"
			}
			c, k := recoveryFixture(t, l, stage)
			transient := true
			k.PrependReactor("get", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				if failure == "stage-api" && transient {
					return true, nil, apierrors.NewServiceUnavailable("retry stage")
				}
				return false, nil, nil
			})
			k.PrependReactor("get", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
				if failure == "logs-api" && transient {
					return true, nil, apierrors.NewServiceUnavailable("retry logs")
				}
				return false, nil, nil
			})
			dir, _ := c.runDir(l.Identity.Run)
			if failure == "stage-filesystem" {
				if err := os.Mkdir(filepath.Join(dir, "guard.json"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := c.Collect(context.Background(), l.Identity.Run)
			if err == nil || got.Phase != "collecting" {
				t.Fatalf("transient collection discarded: %+v, %v", got, err)
			}
			loaded, err := c.Load(l.Identity.Run)
			if err != nil || loaded.Phase != "collecting" {
				t.Fatalf("retry phase not durable: %v", err)
			}
			for _, a := range k.Actions() {
				if a.GetVerb() == "delete" {
					t.Fatal("transient error disposed retry source")
				}
			}
			transient = false
			if failure == "stage-filesystem" {
				if err := os.Remove(filepath.Join(dir, "guard.json")); err != nil {
					t.Fatal(err)
				}
			}
			got, err = c.Collect(context.Background(), l.Identity.Run)
			if err != nil || got.Phase != "complete" {
				t.Fatalf("retry failed: %+v, %v", got, err)
			}
			assertNoLaunch(t, k)
		})
	}
}

func TestTerminalCollectionRejectionAllowsDisposal(t *testing.T) {
	l := ledgerFixture()
	l.Phase = "collecting"
	stage := &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "guard-stage", Namespace: l.RunnerNamespace, UID: types.UID(l.StageUID)}, Data: map[string]string{"evidence.json": "invalid"}}
	c, _ := recoveryFixture(t, l, stage)
	got, err := c.Collect(context.Background(), l.Identity.Run)
	if err != nil || got.Phase != "complete" || got.EvidenceStatus != "rejected" {
		t.Fatalf("terminal evidence rejection: %+v, %v", got, err)
	}
}

func TestTerminalPodWithoutGuardStateEndsMonitor(t *testing.T) {
	for _, phase := range []core.PodPhase{core.PodFailed, core.PodSucceeded} {
		t.Run(string(phase), func(t *testing.T) {
			l := ledgerFixture()
			l.JobUID = "job-uid"
			job := &batch.Job{ObjectMeta: meta.ObjectMeta{Name: "job", Namespace: l.RunnerNamespace, UID: types.UID(l.JobUID), Labels: labels(l)}}
			pod := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: "runner", Namespace: l.RunnerNamespace, UID: "pod-uid", Labels: labels(l), OwnerReferences: []meta.OwnerReference{{Kind: "Job", UID: types.UID(l.JobUID)}}}, Status: core.PodStatus{Phase: phase}}
			c, _ := recoveryFixture(t, l, job, pod)
			terminal, err := c.observePod(context.Background(), l)
			if err != nil || !terminal || l.PodExitCode != nil || !strings.Contains(strings.Join(l.Errors, " "), "runner-guard-end-missing") {
				t.Fatalf("terminal pod was not recorded: %+v, %v", l, err)
			}
		})
	}
}

func TestDisposalTimeoutDoesNotBlockRunnerOrRBAC(t *testing.T) {
	l := ledgerFixture()
	l.Phase, l.RoleUID, l.BindingUID = "disposing", "role-uid", "binding-uid"
	c, k := recoveryFixture(t, l)
	k.PrependReactor("delete", "namespaces", func(a ktesting.Action) (bool, runtime.Object, error) {
		if a.(ktesting.DeleteAction).GetName() == l.ResourceNamespace {
			return true, nil, nil // API accepts DELETE, finalizer keeps namespace.
		}
		return false, nil, nil
	})
	bindingErr := errors.New("binding delete unavailable")
	k.PrependReactor("delete", "clusterrolebindings", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, bindingErr
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	runnerChecked := false
	c.K = contextCheckingClient{Interface: k, check: func(apiCtx context.Context, name string) {
		if name == l.RunnerNamespace {
			runnerChecked = true
			if ctx.Err() == nil || apiCtx.Err() != nil {
				t.Fatal("runner did not get a fresh context after resource timeout")
			}
			deadline, ok := apiCtx.Deadline()
			if !ok || time.Until(deadline) < 29*time.Second || time.Until(deadline) > 30*time.Second {
				t.Fatal("runner did not get its independent 30s budget")
			}
		}
	}}
	err := c.finishDisposal(ctx, l)
	if !runnerChecked {
		t.Fatal("runner cleanup was skipped")
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, bindingErr) {
		t.Fatalf("independent errors not joined: %v", err)
	}
	got, err := c.Load(l.Identity.Run)
	if err != nil || got.ResourceAbsent || !got.RunnerAbsent || got.Phase != "disposing" {
		t.Fatalf("partial disposal flags not durable: %+v, %v", got, err)
	}
	if _, err := k.RbacV1().ClusterRoles().Get(context.Background(), "proof-guard-"+l.Token, meta.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("binding failure blocked role disposal: %v", err)
	}
	for _, a := range k.Actions() {
		if a.GetVerb() == "patch" || a.GetVerb() == "update" {
			t.Fatal("disposal attempted to modify finalizers")
		}
	}
}

func TestStartCancellationDuringProvisionIsDurable(t *testing.T) {
	l := ledgerFixture()
	k := fake.NewClientset(&apps.DaemonSet{ObjectMeta: meta.ObjectMeta{Name: "proof-network-policy-controller", Namespace: "kube-system"}, Status: apps.DaemonSetStatus{NumberReady: 1, DesiredNumberScheduled: 1}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k.PrependReactor("get", "namespaces", func(a ktesting.Action) (bool, runtime.Object, error) {
		cancel()
		return true, nil, context.Canceled
	})
	c := &Coordinator{K: k, Root: t.TempDir(), Out: io.Discard}
	got, err := c.Start(ctx, l.Identity, "cleanup-receipt/runner@sha256:"+strings.Repeat("a", 64), []string{"true"}, time.Second)
	if !errors.Is(err, context.Canceled) || got == nil {
		t.Fatalf("unexpected Start result: %+v, %v", got, err)
	}
	dir, _ := c.runDir(got.Identity.Run)
	if _, err := os.Stat(filepath.Join(dir, "cancel.requested")); err != nil {
		t.Fatal("cancellation was not durable", err)
	}
	loaded, err := c.Load(got.Identity.Run)
	if err != nil || !loaded.CancelRequested || loaded.LaunchAttempted {
		t.Fatalf("invalid cancelled intent: %+v, %v", loaded, err)
	}
}

func TestRecoveryAdoptsExistingAttemptedJobWithoutReplay(t *testing.T) {
	l := ledgerFixture()
	l.Phase, l.LaunchAttempted = "launching", true
	job := &batch.Job{ObjectMeta: meta.ObjectMeta{Name: "job", Namespace: l.RunnerNamespace, UID: "job-uid", Labels: labels(l)}, Status: batch.JobStatus{Conditions: []batch.JobCondition{{Type: batch.JobComplete, Status: core.ConditionTrue}}}}
	c, k := recoveryFixture(t, l, job)
	got, err := c.Collect(context.Background(), l.Identity.Run)
	if err != nil || got.Phase != "complete" || got.JobUID != "job-uid" {
		t.Fatalf("existing attempted Job was not recovered: %+v, %v", got, err)
	}
	assertNoLaunch(t, k)
}

func TestRBACPendingDeletionKeepsDisposalRecoverable(t *testing.T) {
	l := ledgerFixture()
	l.Phase, l.RoleUID, l.BindingUID = "disposing", "role-uid", "binding-uid"
	c, k := recoveryFixture(t, l)
	k.PrependReactor("delete", "clusterroles", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil // Role still exists after successful DELETE.
	})
	err := c.finishDisposal(context.Background(), l)
	if err == nil || l.Phase != "disposing" || !l.ResourceAbsent || !l.RunnerAbsent {
		t.Fatalf("pending role deletion marked complete: %+v, %v", l, err)
	}
}

func TestCommandSerializedLimitBeforePublication(t *testing.T) {
	c := &Coordinator{Root: t.TempDir()}
	// JSON escaping makes this exceed 128 KiB despite only 32 KiB of input.
	_, err := c.Start(context.Background(), ledgerFixture().Identity, "cleanup-receipt/runner@sha256:"+strings.Repeat("a", 64), []string{strings.Repeat("\x01", 16384), strings.Repeat("\x01", 16384)}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "128 KiB") {
		t.Fatalf("serialized command limit not enforced: %v", err)
	}
	entries, _ := os.ReadDir(c.Root)
	if len(entries) != 0 {
		t.Fatal("oversized command published intent")
	}
}

func TestInitialPublicationNeverLeavesEmptyRun(t *testing.T) {
	c := &Coordinator{Root: t.TempDir()}
	l := ledgerFixture()
	l.Command = []string{strings.Repeat("x", 1<<20)}
	if err := c.publish(l); err == nil {
		t.Fatal("oversized ledger published")
	}
	entries, _ := os.ReadDir(c.Root)
	if len(entries) != 0 {
		t.Fatal("failed publication left a directory")
	}
	l.Command = []string{"true"}
	if err := c.publish(l); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Load(l.Identity.Run); err != nil {
		t.Fatal(err)
	}
	l.Command = []string{"replacement"}
	if err := c.publish(l); err == nil {
		t.Fatal("publication replaced existing intent")
	}
	loaded, err := c.Load(l.Identity.Run)
	if err != nil || loaded.Command[0] != "true" {
		t.Fatalf("existing intent changed: %+v, %v", loaded, err)
	}
}

func TestCollectionErrorClassification(t *testing.T) {
	if retryableCollection(terminalCollection(errors.New("invalid"))) || !retryableCollection(errors.New("I/O")) {
		t.Fatal("collection retry classification is unsafe")
	}
	// A previous status cannot turn a later transient error into terminal loss.
	if !retryableCollection(apierrors.NewServiceUnavailable("retry")) {
		t.Fatal("API outage classified terminal")
	}
}
