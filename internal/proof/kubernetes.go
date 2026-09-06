package proof

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Engine struct {
	K         kubernetes.Interface
	D         dynamic.Interface
	Discovery discovery.DiscoveryInterface
}

func Connect(kubeconfig, kubecontext string) (*Engine, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: kubecontext}).ClientConfig()
	if err != nil {
		return nil, err
	}
	cfg.Timeout = 10 * time.Second
	cfg.QPS = 20
	cfg.Burst = 30
	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Engine{k, d, k.Discovery()}, nil
}
func (e *Engine) Begin(ctx context.Context, id Identity, base string) (*State, error) {
	s, err := NewState(id)
	if err != nil {
		return nil, err
	}
	cluster, err := e.K.CoreV1().Namespaces().Get(ctx, "kube-system", meta.GetOptions{})
	if err != nil {
		return nil, err
	}
	s.ClusterUID = string(cluster.UID)
	if err = PrepareSandbox(base, s); err != nil {
		return nil, err
	}
	ns, err := e.K.CoreV1().Namespaces().Create(ctx, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: s.Namespace, Labels: map[string]string{Label: s.Token, "app.kubernetes.io/managed-by": "proofctl"}}}, meta.CreateOptions{})
	if err != nil {
		return nil, err
	}
	s.NamespaceUID = string(ns.UID)
	return s, s.Validate()
}
func (e *Engine) Check(ctx context.Context, s *State) (bool, error) {
	if err := s.Validate(); err != nil {
		return false, err
	}
	cluster, err := e.K.CoreV1().Namespaces().Get(ctx, "kube-system", meta.GetOptions{})
	if err != nil {
		return false, err
	}
	if string(cluster.UID) != s.ClusterUID {
		return false, errors.New("cluster identity changed")
	}
	ns, err := e.K.CoreV1().Namespaces().Get(ctx, s.Namespace, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(ns.UID) != s.NamespaceUID || ns.Labels[Label] != s.Token {
		return false, errors.New("namespace ownership or UID mismatch; refusing cleanup")
	}
	return true, nil
}
func contains(v []string, want string) bool {
	for _, s := range v {
		if s == want {
			return true
		}
	}
	return false
}
func refKey(r Ref) string {
	return r.Group + "/" + r.Resource + "/" + r.Namespace + "/" + r.Name + "/" + r.UID
}
func (e *Engine) Inventory(ctx context.Context, s *State) error {
	s.InventoryComplete = false
	exists, err := e.Check(ctx, s)
	if err != nil {
		s.InventoryComplete = false
		return err
	}
	refs := map[string]Ref{}
	for _, r := range s.Resources {
		refs[refKey(r)] = r
	}
	if exists {
		type discoveryResult struct {
			groups []*meta.APIResourceList
			err    error
		}
		result := make(chan discoveryResult, 1)
		go func() {
			groups, err := e.Discovery.ServerPreferredNamespacedResources()
			result <- discoveryResult{groups, err}
		}()
		var groups []*meta.APIResourceList
		select {
		case <-ctx.Done():
			return ctx.Err()
		case found := <-result:
			groups, err = found.groups, found.err
		}
		if err != nil {
			s.InventoryComplete = false
			return fmt.Errorf("incomplete API discovery: %w", err)
		}
		for _, g := range groups {
			gv, err := schema.ParseGroupVersion(g.GroupVersion)
			if err != nil {
				return err
			}
			for _, r := range g.APIResources {
				if !r.Namespaced || !contains(r.Verbs, "list") {
					continue
				}
				next := ""
				for {
					list, err := e.D.Resource(gv.WithResource(r.Name)).Namespace(s.Namespace).List(ctx, meta.ListOptions{Limit: 500, Continue: next})
					if err != nil {
						s.InventoryComplete = false
						return fmt.Errorf("inventory %s: %w", r.Name, err)
					}
					for _, item := range list.Items {
						ref := Ref{gv.Group, gv.Version, r.Name, s.Namespace, item.GetName(), string(item.GetUID())}
						refs[refKey(ref)] = ref
						if len(refs) > 10000 {
							return errors.New("inventory exceeds 10000 supported references")
						}
					}
					next = list.GetContinue()
					if next == "" {
						break
					}
				}
			}
		}
	}
	// Scan PV claim references as well as labels, so an unlabelled relevant volume
	// is reported as unsupported instead of silently disappearing from coverage.
	next := ""
	for {
		volumes, err := e.K.CoreV1().PersistentVolumes().List(ctx, meta.ListOptions{Limit: 500, Continue: next})
		if err != nil {
			s.InventoryComplete = false
			return fmt.Errorf("volume inventory: %w", err)
		}
		for _, pv := range volumes.Items {
			relevant := pv.Labels[Label] == s.Token || (pv.Spec.ClaimRef != nil && pv.Spec.ClaimRef.Namespace == s.Namespace)
			if !relevant {
				continue
			}
			if pv.Labels[Label] != s.Token {
				return errors.New("unlabelled namespace volume: absence cannot be fully verified")
			}
			if pv.Spec.ClaimRef != nil {
				if pv.Spec.ClaimRef.Namespace != s.Namespace {
					return errors.New("tracked volume belongs to another namespace")
				}
				claimUID := string(pv.Spec.ClaimRef.UID)
				found := false
				for _, r := range refs {
					if r.Resource == "persistentvolumeclaims" && r.Name == pv.Spec.ClaimRef.Name && r.UID == claimUID {
						found = true
					}
				}
				if !found {
					return errors.New("tracked volume lacks a recorded matching claim UID")
				}
			}
			r := Ref{"", "v1", "persistentvolumes", "", pv.Name, string(pv.UID)}
			refs[refKey(r)] = r
		}
		next = volumes.Continue
		if next == "" {
			break
		}
	}
	locations := map[string]string{}
	if len(refs) > 10000 {
		return errors.New("inventory exceeds 10000 supported references")
	}
	for _, r := range refs {
		location := r.Group + "/" + r.Resource + "/" + r.Namespace + "/" + r.Name
		if prior, ok := locations[location]; ok && prior != r.UID {
			return errors.New("resource UID changed since inventory; refusing deletion")
		}
		locations[location] = r.UID
	}
	s.Resources = s.Resources[:0]
	for _, r := range refs {
		s.Resources = append(s.Resources, r)
	}
	sort.Slice(s.Resources, func(i, j int) bool { return refKey(s.Resources[i]) < refKey(s.Resources[j]) })
	s.InventoryComplete = true
	return nil
}
func (e *Engine) resource(r Ref) dynamic.ResourceInterface {
	ri := e.D.Resource(schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource})
	if r.Namespace != "" {
		return ri.Namespace(r.Namespace)
	}
	return ri
}
func (e *Engine) deleteRef(ctx context.Context, s *State, r Ref) error {
	obj, err := e.resource(r).Get(ctx, r.Name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(obj.GetUID()) != r.UID {
		return fmt.Errorf("resource %s/%s replaced: refusing deletion", r.Resource, r.Name)
	}
	if r.Namespace == "" {
		pv, err := e.K.CoreV1().PersistentVolumes().Get(ctx, r.Name, meta.GetOptions{})
		if err != nil {
			return err
		}
		if pv.Labels[Label] != s.Token {
			return errors.New("volume ownership label changed")
		}
		if pv.Spec.ClaimRef != nil {
			if pv.Spec.ClaimRef.Namespace != s.Namespace {
				return errors.New("volume claim namespace changed")
			}
			recorded := false
			for _, known := range s.Resources {
				if known.Resource == "persistentvolumeclaims" && known.Name == pv.Spec.ClaimRef.Name && known.UID == string(pv.Spec.ClaimRef.UID) {
					recorded = true
				}
			}
			if !recorded {
				return errors.New("volume claim UID changed")
			}
			_, err := e.K.CoreV1().PersistentVolumeClaims(s.Namespace).Get(ctx, pv.Spec.ClaimRef.Name, meta.GetOptions{})
			if err == nil {
				return nil
			}
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	uid := types.UID(r.UID)
	policy := meta.DeletePropagationBackground
	err = e.resource(r).Delete(ctx, r.Name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}, PropagationPolicy: &policy})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
func (e *Engine) RemoveResources(ctx context.Context, s *State) error {
	if err := e.Inventory(ctx, s); err != nil {
		return err
	}
	exists, err := e.Check(ctx, s)
	if err != nil {
		return err
	}
	if exists {
		// Mark the owned namespace terminating first. Deleting controller-managed
		// objects in an active namespace can recreate default service accounts and
		// root-CA config maps. Namespace GC removes its objects, then each recorded
		// reference is independently checked for absence below.
		uid := types.UID(s.NamespaceUID)
		policy := meta.DeletePropagationBackground
		err = e.K.CoreV1().Namespaces().Delete(ctx, s.Namespace, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}, PropagationPolicy: &policy})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, r := range s.Resources {
			if r.Namespace == "" {
				if err = e.deleteRef(ctx, s, r); err != nil {
					return err
				}
			}
		}
		exists, err = e.Check(ctx, s)
		if err != nil {
			return err
		}
		absent := !exists
		if absent {
			before := len(s.Resources)
			if err = e.Inventory(ctx, s); err != nil {
				return err
			}
			if len(s.Resources) != before {
				absent = false
			}
		}
		for _, r := range s.Resources {
			live, getErr := e.resource(r).Get(ctx, r.Name, meta.GetOptions{})
			if apierrors.IsNotFound(getErr) {
				continue
			}
			if getErr != nil {
				return getErr
			}
			if string(live.GetUID()) != r.UID {
				return errors.New("tracked resource was replaced during verification")
			}
			absent = false
		}
		if absent {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("cleanup absence not confirmed before deadline: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
func (e *Engine) Cleanup(ctx context.Context, s *State) error {
	s.CleanupAttempts++
	var failures []error
	// Wipe first so an API outage cannot consume the deadline before local cleanup.
	for _, name := range []string{"workspace", "credentials"} {
		err := Wipe(ctx, s, name)
		if err != nil {
			s.Record(name, "failed", err.Error())
			failures = append(failures, err)
		} else {
			s.Record(name, "verified", "Disposable directory is empty; symlink targets were not followed")
		}
	}
	resourceErr := e.RemoveResources(ctx, s)
	if resourceErr != nil {
		s.Record("resources", "failed", resourceErr.Error())
		failures = append(failures, resourceErr)
	} else {
		s.Record("resources", "verified", "Namespace and every inventoried UID confirmed absent")
	}
	if resourceErr != nil {
		s.Record("credentials", "failed", "Kubernetes service-account absence could not be confirmed")
	}
	now := time.Now().UTC()
	s.CompletedAt = &now
	return errors.Join(failures...)
}
