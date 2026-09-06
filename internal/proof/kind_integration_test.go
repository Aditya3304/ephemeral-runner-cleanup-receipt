package proof

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKindCleanup(t *testing.T) {
	config := os.Getenv("PROOF_KIND_CONFIG")
	if config == "" {
		t.Skip("real kind test: use bash dev kind-check")
	}
	e, err := Connect(config, "kind-cleanup-receipt")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	makeRun := func(t *testing.T) *State {
		t.Helper()
		s, err := e.Begin(ctx, Identity{"local", "kind/integration", t.Name(), 1, "job", strings.Repeat("b", 40)}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			// Only the UID created by this test can be removed by the harness.
			current, err := e.K.CoreV1().Namespaces().Get(ctx, s.Namespace, meta.GetOptions{})
			if apierrors.IsNotFound(err) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			if string(current.UID) != s.NamespaceUID {
				t.Error("test namespace replaced; preserving it")
				return
			}
			uid := current.UID
			if err = e.K.CoreV1().Namespaces().Delete(ctx, s.Namespace, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
				t.Error(err)
			}
		})
		return s
	}
	t.Run("namespace_serviceaccount_bound_volume_workspace_and_sentinel", func(t *testing.T) {
		s := makeRun(t)
		sentinel := makeRun(t)
		_, err := e.K.CoreV1().ConfigMaps(sentinel.Namespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "keep"}, Data: map[string]string{"value": "untouched"}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.K.CoreV1().ServiceAccounts(s.Namespace).Create(ctx, &core.ServiceAccount{ObjectMeta: meta.ObjectMeta{Name: "job-sa"}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.K.CoreV1().Secrets(s.Namespace).Create(ctx, &core.Secret{ObjectMeta: meta.ObjectMeta{Name: "job-secret"}, StringData: map[string]string{"example": "disposable-test-value"}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		pvName := s.Namespace + "-volume"
		pv, err := e.K.CoreV1().PersistentVolumes().Create(ctx, &core.PersistentVolume{ObjectMeta: meta.ObjectMeta{Name: pvName, Labels: map[string]string{Label: s.Token}}, Spec: core.PersistentVolumeSpec{Capacity: core.ResourceList{core.ResourceStorage: resource.MustParse("1Mi")}, AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}, PersistentVolumeReclaimPolicy: core.PersistentVolumeReclaimRetain, PersistentVolumeSource: core.PersistentVolumeSource{HostPath: &core.HostPathVolumeSource{Path: "/var/local/cleanup-receipt/" + s.Namespace}}}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			uid := pv.UID
			err := e.K.CoreV1().PersistentVolumes().Delete(ctx, pvName, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}})
			if err != nil && !apierrors.IsNotFound(err) {
				t.Error(err)
			}
		})
		empty := ""
		_, err = e.K.CoreV1().PersistentVolumeClaims(s.Namespace).Create(ctx, &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: "job-volume"}, Spec: core.PersistentVolumeClaimSpec{VolumeName: pvName, StorageClassName: &empty, AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce}, Resources: core.VolumeResourceRequirements{Requests: core.ResourceList{core.ResourceStorage: resource.MustParse("1Mi")}}}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		bound := false
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			claim, err := e.K.CoreV1().PersistentVolumeClaims(s.Namespace).Get(ctx, "job-volume", meta.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if claim.Status.Phase == core.ClaimBound {
				bound = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !bound {
			t.Fatal("PVC never bound to real PV")
		}
		work := filepath.Join(s.Sandbox, "workspace")
		mustWrite(t, filepath.Join(work, ".hidden"), "secret")
		mustWrite(t, filepath.Join(s.Sandbox, "credentials", "token"), "temporary")
		external := filepath.Join(t.TempDir(), "keep")
		mustWrite(t, external, "keep")
		if err := os.Symlink(external, filepath.Join(work, "escape")); err != nil {
			t.Fatal(err)
		}
		if err = e.Inventory(ctx, s); err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, r := range s.Resources {
			found[r.Resource] = true
		}
		for _, kind := range []string{"serviceaccounts", "secrets", "persistentvolumeclaims", "persistentvolumes"} {
			if !found[kind] {
				t.Fatalf("missing %s inventory", kind)
			}
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if err = e.Cleanup(cleanupCtx, s); err != nil {
			t.Fatal(err)
		}
		if _, err = e.K.CoreV1().Namespaces().Get(ctx, s.Namespace, meta.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatal("namespace remains", err)
		}
		if _, err = e.K.CoreV1().ServiceAccounts(s.Namespace).Get(ctx, "job-sa", meta.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatal("serviceaccount remains", err)
		}
		if _, err = e.K.CoreV1().PersistentVolumeClaims(s.Namespace).Get(ctx, "job-volume", meta.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatal("PVC remains", err)
		}
		if _, err = e.K.CoreV1().PersistentVolumes().Get(ctx, pvName, meta.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatal("PV remains", err)
		}
		if _, err = e.K.CoreV1().ConfigMaps(sentinel.Namespace).Get(ctx, "keep", meta.GetOptions{}); err != nil {
			t.Fatal("unrelated resource changed", err)
		}
		if data, err := os.ReadFile(external); err != nil || string(data) != "keep" {
			t.Fatal("external file changed", err)
		}
		if err = e.Cleanup(cleanupCtx, s); err != nil {
			t.Fatal("idempotent cleanup failed", err)
		}
		evidence, err := s.Evidence()
		if err != nil || evidence["verdict"] != "partial" {
			t.Fatal("missing logs/disposal must remain partial", err)
		}
	})
	t.Run("held_finalizer_times_out_and_remains_failure", func(t *testing.T) {
		s := makeRun(t)
		cm, err := e.K.CoreV1().ConfigMaps(s.Namespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "held", Finalizers: []string{"cleanup-receipt.local/test-hold"}}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			live, err := e.K.CoreV1().ConfigMaps(s.Namespace).Get(ctx, "held", meta.GetOptions{})
			if err != nil {
				if !apierrors.IsNotFound(err) {
					t.Error(err)
				}
				return
			}
			if live.UID != cm.UID {
				t.Error("held resource replaced")
				return
			}
			live.Finalizers = nil
			if _, err = e.K.CoreV1().ConfigMaps(s.Namespace).Update(ctx, live, meta.UpdateOptions{}); err != nil {
				t.Error(err)
			}
		})
		short, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err = e.Cleanup(short, s); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("held finalizer did not produce a deadline failure", err)
		}
		live, err := e.K.CoreV1().ConfigMaps(s.Namespace).Get(ctx, "held", meta.GetOptions{})
		if err != nil || len(live.Finalizers) != 1 {
			t.Fatal("cleanup bypassed finalizer", err)
		}
		evidence, _ := s.Evidence()
		if evidence["verdict"] != "fail" {
			t.Fatal("timeout did not produce failure")
		}
	})
	t.Run("namespace_label_change_blocks_deletion", func(t *testing.T) {
		s := makeRun(t)
		ns, err := e.K.CoreV1().Namespaces().Get(ctx, s.Namespace, meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ns.Labels[Label] = "other-owner"
		if _, err = e.K.CoreV1().Namespaces().Update(ctx, ns, meta.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if err = e.RemoveResources(ctx, s); err == nil {
			t.Fatal("ownership change accepted")
		}
		ns, err = e.K.CoreV1().Namespaces().Get(ctx, s.Namespace, meta.GetOptions{})
		if err != nil || ns.DeletionTimestamp != nil {
			t.Fatal("foreign namespace deleted", err)
		}
	})
	t.Run("replacement_resource_is_preserved", func(t *testing.T) {
		s := makeRun(t)
		_, err := e.K.CoreV1().ConfigMaps(s.Namespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "replace"}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.Inventory(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err = e.K.CoreV1().ConfigMaps(s.Namespace).Delete(ctx, "replace", meta.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		replacement, err := e.K.CoreV1().ConfigMaps(s.Namespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "replace"}}, meta.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.RemoveResources(ctx, s); err == nil {
			t.Fatal("replaced resource accepted")
		}
		live, err := e.K.CoreV1().ConfigMaps(s.Namespace).Get(ctx, "replace", meta.GetOptions{})
		if err != nil || live.UID != replacement.UID {
			t.Fatal("replacement was deleted", err)
		}
	})
}
