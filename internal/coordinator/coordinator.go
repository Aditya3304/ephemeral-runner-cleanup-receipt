package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type Coordinator struct {
	GitHub       *GitHubRun
	RunnerConfig string
	VerifyGitHub func(context.Context, *Ledger) error
	K            kubernetes.Interface
	Config       *rest.Config
	Root         string
	Out          io.Writer
}

func Connect(kubeconfig, root string) (*Coordinator, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	cfg.Timeout = 15 * time.Second
	cfg.QPS = 20
	cfg.Burst = 30
	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Coordinator{K: k, Config: cfg, Root: absolute, Out: os.Stdout}, nil
}

func (c *Coordinator) Start(ctx context.Context, id proof.Identity, image string, command []string, timeout time.Duration) (*Ledger, error) {
	if !strings.HasPrefix(image, "cleanup-receipt/runner@sha256:") || len(strings.TrimPrefix(image, "cleanup-receipt/runner@sha256:")) != 64 {
		return nil, errors.New("runner must use the preloaded local image digest")
	}
	if len(command) == 0 || len(command) > 128 || timeout < time.Second || timeout > 10*time.Minute {
		return nil, errors.New("command required; timeout must be 1s to 10m")
	}
	for _, arg := range command {
		if len(arg) > 16384 {
			return nil, errors.New("command argument too long")
		}
	}
	encodedCommand, err := json.Marshal(command)
	if err != nil || len(encodedCommand) > 128<<10 {
		return nil, errors.New("serialized command exceeds 128 KiB limit")
	}
	// Operator setup must install a working network policy implementation first.
	ds, err := c.K.AppsV1().DaemonSets("kube-system").Get(ctx, "proof-network-policy-controller", meta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("network policy controller required: %w", err)
	}
	if ds.Status.NumberReady < 1 || ds.Status.NumberReady != ds.Status.DesiredNumberScheduled {
		return nil, errors.New("network policy controller is not ready")
	}
	// Refuse a new run while a previous intent needs recovery on this small laptop.
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && runPattern.MatchString(entry.Name()) {
			l, e := c.Load(entry.Name())
			if e != nil {
				return nil, e
			}
			if l.Phase != "complete" {
				return nil, fmt.Errorf("run %s requires collect before another run", l.Identity.Run)
			}
		}
	}
	if c.GitHub == nil {
		id.Provider = "local"
		id.Run = "pending"
	} else if id.Provider != "github" {
		return nil, errors.New("GitHub runner requires GitHub identity")
	}
	state, err := proof.NewState(id)
	if err != nil {
		return nil, err
	}
	if c.GitHub == nil {
		id.Run = state.Token
	}
	l := &Ledger{Kind: "local-ci-run/v1", GitHub: c.GitHub, Identity: id, Token: state.Token, ResourceNamespace: state.Namespace, RunnerNamespace: "proof-runner-" + state.Token, Image: image, Command: command, TimeoutSeconds: int64(timeout / time.Second), Phase: "provisioning", CreatedAt: time.Now().UTC(), EvidenceStatus: "missing", LogsStatus: "missing", Errors: []string{}}
	if err = l.ValidateIdentity(); err != nil {
		return nil, err
	}
	if err = c.publish(l); err != nil {
		return l, err
	}
	fmt.Fprintf(c.Out, "Run %s: provisioning isolated local job\n", l.Identity.Run)
	if err = c.provision(ctx, l); err != nil {
		c.problem(l, "provisioning-incomplete", err)
		if ctx.Err() != nil {
			cancelErr := c.RequestCancel(l.Token)
			l.CancelRequested = cancelErr == nil
			err = errors.Join(err, cancelErr)
		}
		return l, errors.Join(err, c.save(l))
	}
	return l, c.monitor(ctx, l)
}

func (c *Coordinator) Collect(ctx context.Context, run string) (*Ledger, error) {
	l, err := c.Load(run)
	if err != nil {
		return nil, err
	}
	if l.Phase == "complete" {
		return l, c.publishObservations(l)
	}
	cluster, err := c.K.CoreV1().Namespaces().Get(ctx, "kube-system", meta.GetOptions{})
	if err != nil {
		return l, err
	}
	if l.ClusterUID != "" && l.ClusterUID != string(cluster.UID) {
		return l, errors.New("cluster identity changed")
	}
	if l.ClusterUID == "" {
		l.ClusterUID = string(cluster.UID)
		if err = c.save(l); err != nil {
			return l, err
		}
	}
	dir, _ := c.runDir(run)
	if _, cancelErr := os.Stat(filepath.Join(dir, "cancel.requested")); cancelErr == nil {
		l.CancelRequested = true
	} else if !errors.Is(cancelErr, os.ErrNotExist) {
		return l, cancelErr
	}
	if l.Phase == "disposing" {
		return l, c.finishDisposal(ctx, l)
	}
	if l.Phase == "collecting" {
		return l, c.finishCollection(ctx, l)
	}
	if l.JobUID == "" {
		job, e := c.K.BatchV1().Jobs(l.RunnerNamespace).Get(ctx, "job", meta.GetOptions{})
		if e == nil {
			if job.UID == "" || job.Labels[proof.Label] != l.Token || job.Labels["app.kubernetes.io/managed-by"] != "localci" {
				return l, errors.New("job ownership mismatch")
			}
			l.JobUID = string(job.UID)
			l.Phase = "running"
			if err = c.save(l); err != nil {
				return l, err
			}
		} else if apierrors.IsNotFound(e) {
			if l.LaunchAttempted || l.CancelRequested {
				if l.LaunchAttempted {
					c.problem(l, "runner-launch-ambiguous", nil)
				} else {
					c.problem(l, "cancelled-before-launch", nil)
				}
				l.Phase = "collecting"
				if err = c.save(l); err != nil {
					return l, err
				}
				return l, c.finishCollection(ctx, l)
			}
			if err = c.provision(ctx, l); err != nil {
				if ctx.Err() != nil {
					cancelErr := c.RequestCancel(run)
					l.CancelRequested = cancelErr == nil
					err = errors.Join(err, cancelErr)
				}
				return l, errors.Join(err, c.save(l))
			}
		} else {
			return l, e
		}
	}
	return l, c.monitor(ctx, l)
}

func (c *Coordinator) monitor(caller context.Context, l *Ledger) error {
	// A user cancellation requests orderly post-processing rather than canceling
	// the very API operations needed to collect evidence and stop the runner.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(l.TimeoutSeconds)*time.Second+5*time.Minute)
	defer cancel()
	if l.Phase == "collecting" {
		return c.finishCollection(ctx, l)
	}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	var nextSignal time.Time
	for {
		if caller.Err() != nil && !l.CancelRequested {
			if err := c.RequestCancel(l.Token); err != nil {
				return err
			}
		}
		dir, _ := c.runDir(l.Token)
		if _, err := os.Stat(filepath.Join(dir, "cancel.requested")); err == nil && !l.CancelRequested {
			l.CancelRequested = true
			if err = c.save(l); err != nil {
				return err
			}
		}
		terminal, err := c.observePod(ctx, l)
		if err != nil {
			c.problem(l, "runner-observation", err)
			return errors.Join(err, c.save(l))
		}
		if terminal {
			break
		}
		if l.CancelRequested && l.PodName != "" && !time.Now().Before(nextSignal) {
			// An exec may succeed before PID 1 installs its SIGTERM handler. Keep
			// requesting cancellation; the guard makes post-processing idempotent.
			if err = c.signal(ctx, l); err != nil {
				c.problem(l, "cancel-signal-retry", err)
			}
			nextSignal = time.Now().Add(time.Second)
		}
		select {
		case <-ctx.Done():
			c.problem(l, "monitor-deadline", ctx.Err())
			_ = c.save(l)
			return ctx.Err()
		case <-ticker.C:
		}
	}
	l.Phase = "collecting"
	if err := c.save(l); err != nil {
		return err
	}
	return c.finishCollection(ctx, l)
}

// terminalCollection is reserved for invalid evidence and irrecoverable limits.
// Ordinary API, stream and filesystem errors must retain the collection source.
type terminalCollectionError struct{ err error }

func (e *terminalCollectionError) Error() string { return e.err.Error() }
func (e *terminalCollectionError) Unwrap() error { return e.err }
func terminalCollection(err error) error         { return &terminalCollectionError{err: err} }

func retryableCollection(err error) bool {
	var terminal *terminalCollectionError
	return err != nil && !apierrors.IsNotFound(err) && !errors.As(err, &terminal)
}

func (c *Coordinator) finishCollection(ctx context.Context, l *Ledger) error {
	if l.GitHub != nil {
		if c.VerifyGitHub == nil {
			c.problem(l, "github-verifier-unavailable", nil)
		} else if err := c.VerifyGitHub(ctx, l); err != nil {
			c.problem(l, "github-verification-incomplete", err)
		}
	}
	var retryErrors []error
	if err := c.collectStage(ctx, l); err != nil {
		c.problem(l, "staging", err)
		if retryableCollection(err) {
			retryErrors = append(retryErrors, err)
		} else if apierrors.IsNotFound(err) {
			l.EvidenceStatus = "missing"
		}
	}
	if err := c.collectLogs(ctx, l); err != nil {
		c.problem(l, "logs", err)
		if retryableCollection(err) {
			retryErrors = append(retryErrors, err)
		}
	}
	if len(retryErrors) > 0 {
		l.Phase = "collecting"
		return errors.Join(errors.Join(retryErrors...), c.save(l))
	}
	l.Phase = "disposing"
	if err := c.save(l); err != nil {
		return err
	}
	return c.finishDisposal(ctx, l)
}

func (c *Coordinator) finishDisposal(ctx context.Context, l *Ledger) error {
	// Disposal is based only on coordinator-owned UIDs, never staged references.
	if err := c.dispose(ctx, l); err != nil {
		c.problem(l, "disposal-incomplete", err)
		return errors.Join(err, c.save(l))
	}
	l.Phase = "complete"
	if err := c.save(l); err != nil {
		return err
	}
	return c.publishObservations(l)
}

func (c *Coordinator) observePod(ctx context.Context, l *Ledger) (bool, error) {
	job, err := c.K.BatchV1().Jobs(l.RunnerNamespace).Get(ctx, "job", meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		c.problem(l, "runner-job-missing", nil)
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if string(job.UID) != l.JobUID || job.Labels[proof.Label] != l.Token {
		return false, errors.New("job UID/ownership mismatch")
	}
	pods, err := c.K.CoreV1().Pods(l.RunnerNamespace).List(ctx, meta.ListOptions{LabelSelector: proof.Label + "=" + l.Token})
	if err != nil {
		return false, err
	}
	if len(pods.Items) > 1 {
		return false, errors.New("unexpected multiple runner pods")
	}
	if len(pods.Items) == 0 {
		if l.PodUID != "" {
			c.problem(l, "runner-pod-missing", nil)
			return true, nil
		}
		for _, condition := range job.Status.Conditions {
			if (condition.Type == "Failed" || condition.Type == "Complete") && condition.Status == core.ConditionTrue {
				c.problem(l, "runner-never-started", nil)
				return true, nil
			}
		}
		return false, nil
	}
	pod := pods.Items[0]
	owned := false
	for _, ref := range pod.OwnerReferences {
		if string(ref.UID) == l.JobUID && ref.Kind == "Job" {
			owned = true
		}
	}
	if !owned || (l.PodUID != "" && l.PodUID != string(pod.UID)) {
		return false, errors.New("runner pod UID/owner mismatch")
	}
	changed := l.PodUID == "" || l.PodName != pod.Name
	l.PodUID = string(pod.UID)
	l.PodName = pod.Name
	// The trusted gate helper must only act after the observed pod identity is
	// durable, so a crash can recover the same pod without adopting a replacement.
	if changed {
		if err = c.save(l); err != nil {
			return false, err
		}
		changed = false
	}
	if pod.Status.PodIP != "" {
		for _, status := range pod.Status.InitContainerStatuses {
			if status.Name == "network-gate" && status.State.Running != nil {
				if err = c.ensureGate(ctx, l, &pod); err != nil {
					return false, err
				}
				break
			}
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "guard" {
			if l.ContainerID != status.ContainerID {
				l.ContainerID = status.ContainerID
				changed = true
			}
			if l.ActualImage != status.ImageID {
				l.ActualImage = status.ImageID
				changed = true
			}
			if status.RestartCount > 0 {
				return false, errors.New("runner restarted unexpectedly")
			}
			if ended := status.State.Terminated; ended != nil {
				l.PodExitCode = &ended.ExitCode
				l.PodReason = ended.Reason
				return true, c.save(l)
			}
		}
	}
	if pod.Status.Phase == core.PodFailed || pod.Status.Phase == core.PodSucceeded {
		l.PodReason = string(pod.Status.Phase)
		c.problem(l, "runner-guard-end-missing", nil)
		return true, c.save(l)
	}
	if changed {
		if err = c.save(l); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (c *Coordinator) signal(ctx context.Context, l *Ledger) error {
	pod, err := c.K.CoreV1().Pods(l.RunnerNamespace).Get(ctx, l.PodName, meta.GetOptions{})
	if err != nil {
		return err
	}
	if string(pod.UID) != l.PodUID {
		return errors.New("pod UID changed before cancellation")
	}
	request := c.K.CoreV1().RESTClient().Post().Resource("pods").Namespace(l.RunnerNamespace).Name(l.PodName).SubResource("exec").VersionedParams(&core.PodExecOptions{Container: "guard", Command: []string{"/usr/local/bin/node", "-e", "process.kill(1,'SIGTERM')"}, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", request.URL())
	if err != nil {
		return err
	}
	signalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.StreamWithContext(signalCtx, remotecommand.StreamOptions{Stdout: io.Discard, Stderr: io.Discard})
}

func (c *Coordinator) deleteNamespace(ctx context.Context, name, uid, token string) error {
	ns, err := c.K.CoreV1().Namespaces().Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(ns.UID) != uid || ns.Labels[proof.Label] != token {
		return errors.New("namespace replacement preserved")
	}
	value := types.UID(uid)
	policy := meta.DeletePropagationForeground
	if err = c.K.CoreV1().Namespaces().Delete(ctx, name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &value}, PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		ns, err = c.K.CoreV1().Namespaces().Get(ctx, name, meta.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if string(ns.UID) != uid {
			return errors.New("namespace replaced during disposal")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// recoverOwnership permits crash-partial adoption only using both coordinator
// labels. Once recorded, an object's UID can never be replaced by adoption.
func (c *Coordinator) recoverOwnership(l *Ledger, obj meta.Object, uid *string) error {
	if obj.GetUID() == "" || obj.GetLabels()[proof.Label] != l.Token {
		return errors.New("object ownership mismatch; replacement preserved")
	}
	if *uid != "" {
		if *uid != string(obj.GetUID()) {
			return errors.New("object UID mismatch; replacement preserved")
		}
		return nil
	}
	if obj.GetLabels()["app.kubernetes.io/managed-by"] != "localci" {
		return errors.New("partial object lacks coordinator ownership; preserved")
	}
	*uid = string(obj.GetUID())
	return c.save(l)
}

func (c *Coordinator) disposeNamespace(ctx context.Context, l *Ledger, name string, uid *string) error {
	obj, err := c.K.CoreV1().Namespaces().Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = c.recoverOwnership(l, obj, uid); err != nil {
		return err
	}
	return c.deleteNamespace(ctx, name, *uid, l.Token)
}

func (c *Coordinator) disposeBinding(ctx context.Context, l *Ledger) error {
	name := "proof-guard-" + l.Token
	binding, err := c.K.RbacV1().ClusterRoleBindings().Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = c.recoverOwnership(l, binding, &l.BindingUID); err != nil {
		return err
	}
	uid := binding.UID
	if err = c.K.RbacV1().ClusterRoleBindings().Delete(ctx, name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	// Successful DELETE can leave an object terminating behind a finalizer.
	if _, err = c.K.RbacV1().ClusterRoleBindings().Get(ctx, name, meta.GetOptions{}); apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		err = errors.New("binding deletion still pending")
	}
	return err
}

func (c *Coordinator) disposeRole(ctx context.Context, l *Ledger) error {
	name := "proof-guard-" + l.Token
	role, err := c.K.RbacV1().ClusterRoles().Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = c.recoverOwnership(l, role, &l.RoleUID); err != nil {
		return err
	}
	uid := role.UID
	if err = c.K.RbacV1().ClusterRoles().Delete(ctx, name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if _, err = c.K.RbacV1().ClusterRoles().Get(ctx, name, meta.GetOptions{}); apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		err = errors.New("role deletion still pending")
	}
	return err
}

func (c *Coordinator) dispose(ctx context.Context, l *Ledger) error {
	identityCtx, identityCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	cluster, err := c.K.CoreV1().Namespaces().Get(identityCtx, "kube-system", meta.GetOptions{})
	identityCancel()
	if err != nil {
		return err
	}
	if string(cluster.UID) != l.ClusterUID {
		return errors.New("cluster changed before disposal")
	}
	// Each cleanup gets its own budget: a resource finalizer must not keep runner
	// credentials alive or prevent either cluster RBAC object from being removed.
	resourceCtx, resourceCancel := context.WithTimeout(ctx, 15*time.Second)
	resourceErr := c.disposeNamespace(resourceCtx, l, l.ResourceNamespace, &l.ResourceUID)
	resourceCancel()
	l.ResourceAbsent = resourceErr == nil

	runnerCtx, runnerCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	runnerErr := c.disposeNamespace(runnerCtx, l, l.RunnerNamespace, &l.RunnerUID)
	runnerCancel()
	l.RunnerAbsent = runnerErr == nil

	bindingCtx, bindingCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	bindingErr := c.disposeBinding(bindingCtx, l)
	l.BindingAbsent = bindingErr == nil
	bindingCancel()
	roleCtx, roleCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	roleErr := c.disposeRole(roleCtx, l)
	l.RoleAbsent = roleErr == nil
	roleCancel()
	return errors.Join(resourceErr, runnerErr, bindingErr, roleErr)
}
