package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const gateNode = "cleanup-receipt-control-plane"

var gateSandboxPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var gateNoncePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func gateRequestNonce(request *core.ConfigMap, l *Ledger) (string, error) {
	if request.UID == "" || request.ResourceVersion == "" || request.DeletionTimestamp != nil || request.Labels[proof.Label] != l.Token || request.Labels["app.kubernetes.io/managed-by"] != "localci" {
		return "", errors.New("network gate request ConfigMap ownership mismatch")
	}
	nonce := request.Data["nonce"]
	if nonce == "" {
		return "", nil
	} // Init has not published its invocation yet.
	if !gateNoncePattern.MatchString(nonce) || request.Data["pod_uid"] != l.PodUID {
		return "", errors.New("invalid network gate request nonce or pod UID")
	}
	return nonce, nil
}

// Separate command construction from execution so tests can model the trusted
// boundary without ever shell-evaluating pod names, IDs, or workload arguments.
func gateDocker(ctx context.Context, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("trusted network gate: %w: %.2048s", err, out)
	}
	if len(out) > 8<<20 {
		return nil, errors.New("CRI listing exceeds limit")
	}
	return out, nil
}

func selectGateSandbox(out []byte, ns, name, uid string) (string, error) {
	var result struct {
		Items []struct {
			ID, State string
			Metadata  struct{ Name, Namespace, UID string }
		}
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", err
	}
	selected := ""
	for _, item := range result.Items {
		// CRI name filters are regexes; require exact metadata after filtering.
		if item.Metadata.Name != name || item.Metadata.Namespace != ns {
			continue
		}
		if item.Metadata.UID != uid {
			return "", errors.New("CRI pod UID mismatch")
		}
		if !gateSandboxPattern.MatchString(item.ID) {
			return "", errors.New("invalid CRI sandbox ID")
		}
		if item.State != "SANDBOX_READY" {
			continue
		}
		if selected != "" {
			return "", errors.New("multiple ready runner sandboxes")
		}
		selected = item.ID
	}
	return selected, nil
}

func (c *Coordinator) ensureGate(ctx context.Context, l *Ledger, pod *core.Pod) error {
	return c.ensureGateWith(ctx, l, pod, gateDocker)
}

func (c *Coordinator) ensureGateWith(ctx context.Context, l *Ledger, pod *core.Pod, run func(context.Context, ...string) ([]byte, error)) error {
	if pod == nil || l.PodUID == "" || string(pod.UID) != l.PodUID || pod.Name != l.PodName || pod.Namespace != l.RunnerNamespace || l.RunnerNamespace != "proof-runner-"+l.Token || !runPattern.MatchString(l.Token) || pod.Labels[proof.Label] != l.Token {
		return errors.New("network gate pod identity mismatch")
	}
	cm, err := c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Get(ctx, "runner-gate", meta.GetOptions{})
	if err != nil {
		return err
	}
	if cm.UID == "" || cm.DeletionTimestamp != nil || cm.Labels[proof.Label] != l.Token || cm.Labels["app.kubernetes.io/managed-by"] != "localci" {
		return errors.New("network gate ConfigMap ownership mismatch")
	}
	if cm.Data["pod_uid"] != "" && cm.Data["pod_uid"] != l.PodUID {
		return errors.New("network gate bound to another pod UID")
	}
	request, err := c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Get(ctx, "runner-gate-request", meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	nonce, err := gateRequestNonce(request, l)
	if err != nil || nonce == "" {
		return err
	}
	if cm.Data["ready"] == "true" {
		if cm.Data["pod_uid"] != l.PodUID {
			return errors.New("ready network gate lacks pod UID binding")
		}
		// A new sandbox/init invocation must publish a fresh nonce BEFORE it
		// checks readiness. A marker from an earlier invocation cannot release it.
		if cm.Data["nonce"] == nonce {
			return nil
		}
	}
	if cm.Data["ready"] != "" && cm.Data["ready"] != "false" && cm.Data["ready"] != "true" {
		return errors.New("invalid network gate ready state")
	}
	if pod.DeletionTimestamp != nil {
		return nil
	}
	if pod.Spec.HostNetwork {
		return errors.New("runner host networking refused")
	}
	if pod.Spec.NodeName == "" || pod.Status.PodIP == "" {
		return nil
	}
	if pod.Spec.NodeName != gateNode {
		return errors.New("network gate supports only the fixed kind control-plane node")
	}
	waiting := false
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == "network-gate" && status.State.Running != nil {
			waiting = true
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Running != nil || status.State.Terminated != nil {
			return errors.New("workload started before network gate release")
		}
	}
	if !waiting {
		return nil
	}
	nodes, err := c.K.CoreV1().Nodes().List(ctx, meta.ListOptions{})
	if err != nil {
		return err
	}
	if len(nodes.Items) != 1 || nodes.Items[0].Name != gateNode {
		return errors.New("network gate requires the single-node cleanup-receipt cluster")
	}
	svc, err := c.K.CoreV1().Services("default").Get(ctx, "kubernetes", meta.GetOptions{})
	if err != nil {
		return err
	}
	apiIP, err := netip.ParseAddr(svc.Spec.ClusterIP)
	if err != nil || !apiIP.Is4() || !apiIP.IsPrivate() {
		return errors.New("network gate requires a private IPv4 API Service")
	}
	endpoint := ""
	for _, address := range nodes.Items[0].Status.Addresses {
		ip, e := netip.ParseAddr(address.Address)
		if address.Type == core.NodeInternalIP && e == nil && ip.Is4() && ip.IsPrivate() {
			if endpoint != "" && endpoint != ip.String() {
				return errors.New("ambiguous node IPv4 addresses")
			}
			endpoint = ip.String()
		}
	}
	if endpoint == "" {
		return errors.New("no private IPv4 control-plane endpoint")
	}
	listed, err := run(ctx, "exec", gateNode, "/usr/local/bin/crictl", "--runtime-endpoint", "unix:///run/containerd/containerd.sock", "pods", "--namespace", l.RunnerNamespace, "--name", pod.Name, "-o", "json")
	if err != nil {
		return err
	}
	sandbox, err := selectGateSandbox(listed, l.RunnerNamespace, pod.Name, l.PodUID)
	if err != nil || sandbox == "" {
		return err
	}
	_, err = run(ctx, "exec", gateNode, "/usr/local/bin/proof-netgate", "--sandbox", sandbox, "--pod-uid", l.PodUID, "--namespace", l.RunnerNamespace, "--api-ip", apiIP.String(), "--endpoint-ip", endpoint)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 75 {
			return nil
		}
		return err
	}
	// Production connections must arm the detached trusted collector while this
	// exact init invocation still holds the workload. Fake API clients in the
	// network-gate unit tests do not have an external kind runtime.
	if c.Config != nil {
		if err = c.armCollector(ctx, l, sandbox); err != nil {
			return err
		}
	}
	latest, err := c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Get(ctx, "runner-gate-request", meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	latestNonce, err := gateRequestNonce(latest, l)
	if err != nil {
		return err
	}
	if latest.UID != request.UID || latest.ResourceVersion != request.ResourceVersion || latestNonce != nonce || latest.Data["pod_uid"] != request.Data["pod_uid"] {
		return nil // Request changed during verification; re-probe on the next poll.
	}
	// UID/resourceVersion tests prevent releasing a concurrently replaced gate.
	// There is no cross-ConfigMap transaction. If the request changes after the
	// read above, the new init nonce still cannot accept this older-nonce marker.
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(cm.UID)},
		{"op": "test", "path": "/metadata/resourceVersion", "value": cm.ResourceVersion},
		{"op": "add", "path": "/data", "value": map[string]string{"ready": "true", "pod_uid": l.PodUID, "nonce": nonce}},
	})
	if err != nil {
		return err
	}
	_, err = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Patch(ctx, "runner-gate", types.JSONPatchType, patch, meta.PatchOptions{})
	return err
}
