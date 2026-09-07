package coordinator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator/observer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"golang.org/x/sys/unix"
)

func collectorBinding(l *Ledger) string {
	data, _ := proof.Canonical(struct {
		Identity                                                                                                                           proof.Identity `json:"identity"`
		Cluster, ResourceNamespace, ResourceUID, RunnerNamespace, RunnerUID, JobUID, PodName, PodUID, StageUID, RoleUID, BindingUID, Image string
		Command                                                                                                                            []string
	}{l.Identity, l.ClusterUID, l.ResourceNamespace, l.ResourceUID, l.RunnerNamespace, l.RunnerUID, l.JobUID, l.PodName, l.PodUID, l.StageUID, l.RoleUID, l.BindingUID, l.Image, l.Command})
	return proof.Digest(data)
}
func collectorArgs(r observer.Request, mode string) []string {
	return []string{"/usr/local/bin/proof-observe", "--mode", mode, "--run", r.Run, "--pod", r.Pod, "--uid", r.UID, "--sandbox", r.Sandbox, "--binding", r.Binding, "--seconds", strconv.Itoa(r.Seconds)}
}

type cappedWriter struct {
	W         io.Writer
	Remaining int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.Remaining {
		return 0, errors.New("collector transport byte limit exceeded")
	}
	n, e := w.W.Write(p)
	w.Remaining -= int64(n)
	return n, e
}
func collectorCommand(ctx context.Context, r observer.Request, mode string, w io.Writer) error {
	if e := r.Validate(); e != nil {
		return e
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"exec", observer.Node}
	if mode == "collect" {
		args = []string{"exec", "-d", observer.Node}
	}
	cmd := exec.CommandContext(bounded, "docker", append(args, collectorArgs(r, mode)...)...)
	limit := int64(64 << 10)
	if mode == "logs" {
		limit = observer.MaxLogs
	}
	cmd.Stdout = &cappedWriter{w, limit}
	var stderr bytes.Buffer
	cmd.Stderr = &cappedWriter{&stderr, 2048}
	if e := cmd.Run(); e != nil {
		return classifyCollectorFailure(mode, e, stderr.String())
	}
	return nil
}

func classifyCollectorFailure(mode string, err error, stderr string) error {
	wrapped := fmt.Errorf("trusted collector %s: %w: %.2048s", mode, err, stderr)
	var exit *exec.ExitError
	if mode == "result" && errors.As(err, &exit) && exit.ExitCode() == 66 {
		return terminalCollection(wrapped)
	}
	return wrapped
}
func (c *Coordinator) armCollector(ctx context.Context, l *Ledger, sandbox string) error {
	r := observer.Request{Run: l.Token, Pod: l.PodName, UID: l.PodUID, Sandbox: sandbox, Binding: collectorBinding(l), Seconds: int(l.TimeoutSeconds) + 300}
	if e := r.Validate(); e != nil {
		return e
	}
	if l.CollectorRequest != nil && *l.CollectorRequest != r {
		return errors.New("collector binding changed; gate remains closed")
	}
	if l.CollectorRequest == nil {
		l.CollectorRequest = &r
		if e := c.save(l); e != nil {
			return e
		}
	}
	var ready bytes.Buffer
	if e := collectorCommand(ctx, r, "ready", &ready); e != nil {
		if e = collectorCommand(ctx, r, "collect", io.Discard); e != nil {
			return e
		}
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			ready.Reset()
			if e = collectorCommand(ctx, r, "ready", &ready); e == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return e
			case <-tick.C:
			}
		}
	}
	var armed observer.Result
	if e := strict(ready.Bytes(), &armed); e != nil {
		return e
	}
	if armed.Request != r || !armed.StartProven || armed.ArmedAt.IsZero() {
		return errors.New("collector did not prove a pre-start watch")
	}
	return nil
}
func (c *Coordinator) collectTrusted(ctx context.Context, l *Ledger) error {
	r := *l.CollectorRequest
	if r.Binding != collectorBinding(l) {
		return terminalCollection(errors.New("collector ledger binding mismatch"))
	}
	dir, e := c.runDir(l.Identity.Run)
	if e != nil {
		return e
	}
	// A completed transfer is immutable across retries and never depends on a
	// subsequently deleted pod, node spool, or mutable staging ConfigMap.
	if l.CollectorDigest != "" {
		raw, e := proof.ReadBounded(filepath.Join(dir, "collector.json"), 64<<10)
		if e != nil {
			return e
		}
		if proof.Digest(raw) != l.CollectorDigest {
			return errors.New("collector artifact changed")
		}
		return verifyFile(filepath.Join(dir, "job.log"), l.LogsDigest, l.LogsBytes, observer.MaxLogs)
	}
	var raw bytes.Buffer
	if e = collectorCommand(ctx, r, "result", &raw); e != nil {
		var terminal *terminalCollectionError
		if errors.As(e, &terminal) {
			l.LogsStatus = "missing"
		}
		return e
	}
	var result observer.Result
	if e = strict(raw.Bytes(), &result); e != nil {
		return e
	}
	if e = result.Validate(); e != nil {
		return e
	}
	if result.Request != r {
		return errors.New("collector result binding mismatch")
	}
	if result.TerminationProven && (l.ContainerID != "containerd://"+result.ContainerID || l.ActualImage != result.ImageID || l.PodExitCode == nil || *l.PodExitCode != result.ExitCode) {
		return terminalCollection(errors.New("API/CRI terminal identity mismatch"))
	}
	f, e := os.CreateTemp(dir, ".collector-log-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	e = collectorCommand(ctx, r, "logs", f)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	if e = verifyFile(f.Name(), result.LogsDigest, result.LogsBytes, observer.MaxLogs); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), filepath.Join(dir, "job.log")); e != nil {
		return e
	}
	data, e := proof.Canonical(result)
	if e != nil {
		return e
	}
	if e = durable(filepath.Join(dir, "collector.json"), data); e != nil {
		return e
	}
	l.CollectorDigest = proof.Digest(data)
	l.LogsDigest = result.LogsDigest
	l.LogsBytes = result.LogsBytes
	l.LogsStatus = "gap"
	if result.Logs.Status == "verified" {
		l.LogsStatus = "complete"
	}
	return c.save(l)
}
func verifyFile(path, digest string, size, limit int64) error {
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() != size || size > limit || size < 0 {
		return errors.New("artifact byte size mismatch")
	}
	hash := sha256.New()
	count, e := io.Copy(hash, io.LimitReader(f, limit+1))
	if e != nil {
		return e
	}
	if count != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("artifact digest mismatch")
	}
	return nil
}
