package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	network "k8s.io/api/networking/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func ptr[T any](v T) *T { return &v }
func labels(l *Ledger) map[string]string {
	return map[string]string{proof.Label: l.Token, "app.kubernetes.io/managed-by": "localci"}
}
func nsLabels(l *Ledger) map[string]string {
	m := labels(l)
	m["pod-security.kubernetes.io/enforce"] = "restricted"
	m["pod-security.kubernetes.io/enforce-version"] = "v1.36"
	return m
}

// Provision resumes only owned objects under a previously persisted intent. It
// never replaces objects, adopts a foreign namespace, or launches a second Job.
func (c *Coordinator) provision(ctx context.Context, l *Ledger) error {
	cluster, err := c.K.CoreV1().Namespaces().Get(ctx, "kube-system", meta.GetOptions{})
	if err != nil {
		return err
	}
	if l.ClusterUID != "" && l.ClusterUID != string(cluster.UID) {
		return errors.New("cluster identity changed")
	}
	l.ClusterUID = string(cluster.UID)
	if err = c.save(l); err != nil {
		return err
	}
	for _, entry := range []struct {
		name string
		uid  *string
	}{{l.RunnerNamespace, &l.RunnerUID}, {l.ResourceNamespace, &l.ResourceUID}} {
		obj, err := c.K.CoreV1().Namespaces().Get(ctx, entry.name, meta.GetOptions{})
		if apierrors.IsNotFound(err) {
			obj, err = c.K.CoreV1().Namespaces().Create(ctx, &core.Namespace{ObjectMeta: meta.ObjectMeta{Name: entry.name, Labels: nsLabels(l)}}, meta.CreateOptions{})
		}
		if err != nil {
			return err
		}
		if obj.Labels[proof.Label] != l.Token || obj.Labels["app.kubernetes.io/managed-by"] != "localci" || (*entry.uid != "" && *entry.uid != string(obj.UID)) || obj.DeletionTimestamp != nil {
			return errors.New("namespace intent ownership/UID mismatch")
		}
		if obj.Labels["pod-security.kubernetes.io/enforce"] != "restricted" || obj.Labels["pod-security.kubernetes.io/enforce-version"] != "v1.36" {
			return errors.New("namespace admission policy changed")
		}
		*entry.uid = string(obj.UID)
		if err = c.save(l); err != nil {
			return err
		}
	}
	// Admission and quotas are coordinator-controlled. Jobs cannot create pods,
	// volume claims, services, role bindings, or policy objects through their token.
	for _, ns := range []string{l.RunnerNamespace, l.ResourceNamespace} {
		quota := &core.ResourceQuota{ObjectMeta: meta.ObjectMeta{Name: "run-budget", Namespace: ns, Labels: labels(l)}, Spec: core.ResourceQuotaSpec{Hard: core.ResourceList{
			"count/pods": resource.MustParse("1"), "count/secrets": resource.MustParse("8"), "count/configmaps": resource.MustParse("16"), "count/serviceaccounts": resource.MustParse("8"), "persistentvolumeclaims": resource.MustParse("0"),
		}}}
		if _, err = c.K.CoreV1().ResourceQuotas(ns).Create(ctx, quota, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		policy := &network.NetworkPolicy{ObjectMeta: meta.ObjectMeta{Name: "isolate-run", Namespace: ns, Labels: labels(l)}, Spec: network.NetworkPolicySpec{PodSelector: meta.LabelSelector{}, PolicyTypes: []network.PolicyType{network.PolicyTypeIngress, network.PolicyTypeEgress}}}
		if ns == l.RunnerNamespace {
			// Only the exact local API endpoints are permitted. No DNS or public egress.
			svc, err := c.K.CoreV1().Services("default").Get(ctx, "kubernetes", meta.GetOptions{})
			if err != nil {
				return err
			}
			nodes, err := c.K.CoreV1().Nodes().List(ctx, meta.ListOptions{})
			if err != nil {
				return err
			}
			if len(nodes.Items) != 1 {
				return errors.New("this local CI profile supports exactly one kind node")
			}
			hosts := []string{svc.Spec.ClusterIP}
			for _, n := range nodes.Items {
				for _, a := range n.Status.Addresses {
					if a.Type == core.NodeInternalIP {
						hosts = append(hosts, a.Address)
					}
				}
			}
			for _, host := range hosts {
				bits := "/32"
				if net.ParseIP(host) == nil {
					return errors.New("invalid API IP")
				}
				if net.ParseIP(host).To4() == nil {
					bits = "/128"
				}
				port := int32(6443)
				if host == svc.Spec.ClusterIP {
					port = 443
				}
				policy.Spec.Egress = append(policy.Spec.Egress, network.NetworkPolicyEgressRule{To: []network.NetworkPolicyPeer{{IPBlock: &network.IPBlock{CIDR: host + bits}}}, Ports: []network.NetworkPolicyPort{{Protocol: ptr(core.ProtocolTCP), Port: ptr(intstr.FromInt32(port))}}})
			}
			if l.GitHub != nil {
				policy.Spec.Egress = append(policy.Spec.Egress, network.NetworkPolicyEgressRule{To: []network.NetworkPolicyPeer{{IPBlock: &network.IPBlock{CIDR: l.GitHub.ProxyIP + "/32"}}}, Ports: []network.NetworkPolicyPort{{Protocol: ptr(core.ProtocolTCP), Port: ptr(intstr.FromInt32(3128))}}})
			}
		}
		if _, err = c.K.NetworkingV1().NetworkPolicies(ns).Create(ctx, policy, meta.CreateOptions{}); apierrors.IsAlreadyExists(err) {
			live, getErr := c.K.NetworkingV1().NetworkPolicies(ns).Get(ctx, policy.Name, meta.GetOptions{})
			if getErr != nil {
				return getErr
			}
			if live.Labels[proof.Label] != l.Token || !reflect.DeepEqual(live.Spec, policy.Spec) {
				return errors.New("network policy ownership/spec changed")
			}
		} else if err != nil {
			return err
		}
	}
	sa := &core.ServiceAccount{ObjectMeta: meta.ObjectMeta{Name: "guard", Namespace: l.RunnerNamespace, Labels: labels(l)}, AutomountServiceAccountToken: ptr(false)}
	if _, err = c.K.CoreV1().ServiceAccounts(l.RunnerNamespace).Create(ctx, sa, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	stage, err := c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "guard-stage", Namespace: l.RunnerNamespace, Labels: labels(l)}, Data: map[string]string{}}, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		stage, err = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Get(ctx, "guard-stage", meta.GetOptions{})
	}
	if err != nil {
		return err
	}
	if stage.Labels[proof.Label] != l.Token || (l.StageUID != "" && l.StageUID != string(stage.UID)) {
		return errors.New("stage UID changed")
	}
	l.StageUID = string(stage.UID)
	if _, err = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "runner-gate", Namespace: l.RunnerNamespace, Labels: labels(l)}, Data: map[string]string{"ready": "false"}}, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if _, err = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "runner-gate-request", Namespace: l.RunnerNamespace, Labels: labels(l)}, Data: map[string]string{}}, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err = c.save(l); err != nil {
		return err
	}
	subject := []rbac.Subject{{Kind: "ServiceAccount", Name: "guard", Namespace: l.RunnerNamespace}}
	for _, scope := range []struct {
		namespace, name string
		rules           []rbac.PolicyRule
	}{
		{l.ResourceNamespace, "guard-resources", []rbac.PolicyRule{
			{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"configmaps", "secrets", "serviceaccounts"}, Verbs: []string{"create", "delete"}},
		}},
		{l.RunnerNamespace, "guard-stage", []rbac.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{"guard-stage", "runner-gate-request"}, Verbs: []string{"get", "patch"}}, {APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{"runner-gate"}, Verbs: []string{"get"}}}},
	} {
		if _, err = c.K.RbacV1().Roles(scope.namespace).Create(ctx, &rbac.Role{ObjectMeta: meta.ObjectMeta{Name: scope.name, Namespace: scope.namespace, Labels: labels(l)}, Rules: scope.rules}, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if _, err = c.K.RbacV1().RoleBindings(scope.namespace).Create(ctx, &rbac.RoleBinding{ObjectMeta: meta.ObjectMeta{Name: scope.name, Namespace: scope.namespace, Labels: labels(l)}, Subjects: subject, RoleRef: rbac.RoleRef{APIGroup: rbac.GroupName, Kind: "Role", Name: scope.name}}, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	roleName := "proof-guard-" + l.Token
	role, err := c.K.RbacV1().ClusterRoles().Create(ctx, &rbac.ClusterRole{ObjectMeta: meta.ObjectMeta{Name: roleName, Labels: labels(l)}, Rules: []rbac.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{"kube-system", l.ResourceNamespace}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{l.ResourceNamespace}, Verbs: []string{"delete"}},
		{APIGroups: []string{""}, Resources: []string{"persistentvolumes"}, Verbs: []string{"get", "list"}},
	}}, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		role, err = c.K.RbacV1().ClusterRoles().Get(ctx, roleName, meta.GetOptions{})
	}
	if err != nil {
		return err
	}
	if role.Labels[proof.Label] != l.Token || (l.RoleUID != "" && l.RoleUID != string(role.UID)) {
		return errors.New("role UID changed")
	}
	l.RoleUID = string(role.UID)
	binding, err := c.K.RbacV1().ClusterRoleBindings().Create(ctx, &rbac.ClusterRoleBinding{ObjectMeta: meta.ObjectMeta{Name: roleName, Labels: labels(l)}, Subjects: subject, RoleRef: rbac.RoleRef{APIGroup: rbac.GroupName, Kind: "ClusterRole", Name: roleName}}, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		binding, err = c.K.RbacV1().ClusterRoleBindings().Get(ctx, roleName, meta.GetOptions{})
	}
	if err != nil {
		return err
	}
	if binding.Labels[proof.Label] != l.Token || (l.BindingUID != "" && l.BindingUID != string(binding.UID)) {
		return errors.New("binding UID changed")
	}
	l.BindingUID = string(binding.UID)
	if err = c.save(l); err != nil {
		return err
	}
	assignment := map[string]any{"kind": "cleanup-assignment/v1", "identity": l.Identity, "token": l.Token, "namespace": l.ResourceNamespace, "namespace_uid": l.ResourceUID, "cluster_uid": l.ClusterUID}
	assignmentJSON, err := proof.Canonical(assignment)
	if err != nil {
		return err
	}
	svc, err := c.K.CoreV1().Services("default").Get(ctx, "kubernetes", meta.GetOptions{})
	if err != nil {
		return err
	}
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: local\n  cluster:\n    server: https://%s:443\n    certificate-authority: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt\nusers:\n- name: guard\n  user:\n    tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token\ncontexts:\n- name: assigned\n  context:\n    cluster: local\n    user: guard\ncurrent-context: assigned\n", svc.Spec.ClusterIP)
	if _, err = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Create(ctx, &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "assignment", Namespace: l.RunnerNamespace, Labels: labels(l)}, Immutable: ptr(true), Data: map[string]string{"assignment.json": string(assignmentJSON), "kubeconfig": kubeconfig}}, meta.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	dir, _ := c.runDir(l.Token)
	if _, cancelErr := os.Stat(filepath.Join(dir, "cancel.requested")); cancelErr == nil {
		l.CancelRequested = true
		l.Phase = "collecting"
		return c.save(l)
	}
	if l.LaunchAttempted {
		return errors.New("launch was already attempted; reconcile without repeating the command")
	}
	l.Phase = "launching"
	l.LaunchAttempted = true
	if err = c.save(l); err != nil {
		return err
	}
	job := c.job(l)
	if l.GitHub != nil {
		if len(c.RunnerConfig) == 0 || len(c.RunnerConfig) > 64<<10 {
			return errors.New("bounded private GitHub JIT configuration required")
		}
		secret := &core.Secret{ObjectMeta: meta.ObjectMeta{Name: "github-jit", Namespace: l.RunnerNamespace, Labels: labels(l)}, Immutable: ptr(true), Data: map[string][]byte{"config": []byte(c.RunnerConfig)}}
		if _, e := c.K.CoreV1().Secrets(l.RunnerNamespace).Create(ctx, secret, meta.CreateOptions{}); e != nil {
			return e
		}
		c.configureGitHubJob(job, l)
	}
	created, err := c.K.BatchV1().Jobs(l.RunnerNamespace).Create(ctx, job, meta.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = c.K.BatchV1().Jobs(l.RunnerNamespace).Get(ctx, "job", meta.GetOptions{})
	}
	if err != nil {
		return err
	}
	if created.Labels[proof.Label] != l.Token || (l.JobUID != "" && l.JobUID != string(created.UID)) {
		return errors.New("job UID changed")
	}
	l.JobUID = string(created.UID)
	l.Phase = "running"
	return c.save(l)
}

func (c *Coordinator) job(l *Ledger) *batch.Job {
	env := []core.EnvVar{
		{Name: "PROOF_CTL", Value: "/usr/local/bin/proofctl"}, {Name: "PROOF_ASSIGNMENT", Value: "/assignment/assignment.json"},
		{Name: "PROOF_KUBECONFIG", Value: "/assignment/kubeconfig"}, {Name: "PROOF_STATE", Value: "/control/context.json"},
		{Name: "PROOF_SANDBOX_ROOT", Value: "/data"}, {Name: "PROOF_TRANSPORT", Value: "kubernetes"},
		{Name: "PROOF_STAGE_NAMESPACE", Value: l.RunnerNamespace}, {Name: "PROOF_STAGE_NAME", Value: "guard-stage"},
		{Name: "PROOF_JOB_TIMEOUT_MS", Value: strconv.FormatInt(l.TimeoutSeconds*1000, 10)},
		{Name: "PROOF_CLEANUP_TIMEOUT_MS", Value: "120000"},
		{Name: "PROOF_POD_UID", ValueFrom: &core.EnvVarSource{FieldRef: &core.ObjectFieldSelector{FieldPath: "metadata.uid"}}},
	}
	args := append([]string{"/opt/guard/dist/local.js", "--"}, l.Command...)
	return &batch.Job{ObjectMeta: meta.ObjectMeta{Name: "job", Namespace: l.RunnerNamespace, Labels: labels(l)}, Spec: batch.JobSpec{
		BackoffLimit: ptr(int32(0)), Completions: ptr(int32(1)), Parallelism: ptr(int32(1)), ActiveDeadlineSeconds: ptr(l.TimeoutSeconds + 240),
		Template: core.PodTemplateSpec{ObjectMeta: meta.ObjectMeta{Labels: labels(l)}, Spec: core.PodSpec{
			ServiceAccountName: "guard", AutomountServiceAccountToken: ptr(false), RestartPolicy: core.RestartPolicyNever, TerminationGracePeriodSeconds: ptr(int64(150)),
			SecurityContext: &core.PodSecurityContext{RunAsNonRoot: ptr(true), RunAsUser: ptr(int64(1000)), RunAsGroup: ptr(int64(1000)), FSGroup: ptr(int64(1000)), SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault}},
			InitContainers: []core.Container{{Name: "network-gate", Image: l.Image, ImagePullPolicy: core.PullNever, Command: []string{"/usr/local/bin/node", "/opt/examples/network-gate.mjs"}, Env: env,
				SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: ptr(false), ReadOnlyRootFilesystem: ptr(true), Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}},
				Resources:       core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("50m"), core.ResourceMemory: resource.MustParse("32Mi")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("200m"), core.ResourceMemory: resource.MustParse("64Mi")}},
				VolumeMounts:    []core.VolumeMount{{Name: "api-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true}},
			}},
			Containers: []core.Container{{Name: "guard", Image: l.Image, ImagePullPolicy: core.PullNever, Command: []string{"/usr/local/bin/node"}, Args: args, Env: env,
				SecurityContext: &core.SecurityContext{AllowPrivilegeEscalation: ptr(false), ReadOnlyRootFilesystem: ptr(true), Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}},
				Resources:       core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("128Mi")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("1"), core.ResourceMemory: resource.MustParse("384Mi"), core.ResourceEphemeralStorage: resource.MustParse("256Mi")}},
				VolumeMounts:    []core.VolumeMount{{Name: "work", MountPath: "/data"}, {Name: "control", MountPath: "/control"}, {Name: "tmp", MountPath: "/tmp"}, {Name: "assignment", MountPath: "/assignment/assignment.json", SubPath: "assignment.json", ReadOnly: true}, {Name: "assignment", MountPath: "/assignment/kubeconfig", SubPath: "kubeconfig", ReadOnly: true}, {Name: "api-token", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true}},
			}},
			Volumes: []core.Volume{
				{Name: "work", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("128Mi"))}}},
				{Name: "control", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("16Mi"))}}},
				{Name: "tmp", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("32Mi"))}}},
				{Name: "assignment", VolumeSource: core.VolumeSource{ConfigMap: &core.ConfigMapVolumeSource{LocalObjectReference: core.LocalObjectReference{Name: "assignment"}, DefaultMode: ptr(int32(0444))}}},
				{Name: "api-token", VolumeSource: core.VolumeSource{Projected: &core.ProjectedVolumeSource{DefaultMode: ptr(int32(0440)), Sources: []core.VolumeProjection{
					{ServiceAccountToken: &core.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: ptr(int64(600))}},
					{ConfigMap: &core.ConfigMapProjection{LocalObjectReference: core.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []core.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
				}}}},
			},
		}},
	}}
}

// JSONSummary intentionally omits command strings and raw job output.
func JSONSummary(l *Ledger) []byte {
	data, _ := json.MarshalIndent(map[string]any{"run_id": l.Identity.Run, "phase": l.Phase, "pod_exit_code": l.PodExitCode, "cancel_requested": l.CancelRequested, "evidence": l.EvidenceStatus, "logs": l.LogsStatus, "resources_absent": l.ResourceAbsent, "runner_absent": l.RunnerAbsent, "errors": l.Errors}, "", "  ")
	return data
}
