//go:build linux

package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("trusted command exceeded byte limit")
	}
	return b.Buffer.Write(p)
}
func cri(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/local/bin/crictl", append([]string{"--runtime-endpoint", "unix:///run/containerd/containerd.sock"}, args...)...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	out := &limitedBuffer{limit: 8 << 20}
	stderr := &limitedBuffer{limit: 2048}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("CRI observation: %w", err)
	}
	return out.Bytes(), nil
}

type runtimeContainer struct {
	ID           string `json:"id"`
	PodSandboxID string `json:"podSandboxId"`
	State        string `json:"state"`
	Metadata     struct {
		Name    string `json:"name"`
		Attempt uint32 `json:"attempt"`
	} `json:"metadata"`
	Labels     map[string]string `json:"labels"`
	ImageRef   string            `json:"imageRef"`
	LogPath    string            `json:"logPath"`
	StartedAt  json.RawMessage   `json:"startedAt"`
	FinishedAt json.RawMessage   `json:"finishedAt"`
	ExitCode   int32             `json:"exitCode"`
}

// crictl's human JSON formatter uses RFC3339Nano; raw CRI JSON may use
// decimal nanoseconds. Neither format is rounded through float64.
func runtimeTime(raw json.RawMessage) (int64, error) {
	value := string(raw)
	if len(value) > 0 && value[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
		if stamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
			if stamp.Year() < 2000 || stamp.Year() > 2200 {
				return 0, errors.New("runtime timestamp out of range")
			}
			return stamp.UnixNano(), nil
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func containers(ctx context.Context, r Request) ([]runtimeContainer, error) {
	data, e := cri(ctx, "ps", "-a", "--pod", r.Sandbox, "-o", "json")
	if e != nil {
		return nil, e
	}
	var v struct {
		Containers []runtimeContainer `json:"containers"`
	}
	if e = json.Unmarshal(data, &v); e != nil {
		return nil, e
	}
	for _, c := range v.Containers {
		if c.PodSandboxID != r.Sandbox || c.Labels["io.kubernetes.pod.uid"] != r.UID || c.Labels["io.kubernetes.pod.namespace"] != r.Namespace() || !hex64.MatchString(c.ID) {
			return nil, errors.New("CRI container identity mismatch")
		}
	}
	return v.Containers, nil
}
func checkBeforeStart(ctx context.Context, r Request) error {
	data, e := cri(ctx, "inspectp", r.Sandbox)
	if e != nil {
		return e
	}
	var v struct {
		Status struct {
			ID, State string
			Metadata  struct{ Name, Namespace, UID string }
		}
	}
	if e = json.Unmarshal(data, &v); e != nil {
		return e
	}
	if v.Status.ID != r.Sandbox || v.Status.State != "SANDBOX_READY" || v.Status.Metadata.UID != r.UID || v.Status.Metadata.Name != r.Pod || v.Status.Metadata.Namespace != r.Namespace() {
		return errors.New("CRI sandbox identity mismatch")
	}
	cs, e := containers(ctx, r)
	if e != nil {
		return e
	}
	waiting := false
	for _, c := range cs {
		if c.Metadata.Name != "network-gate" {
			return errors.New("guard already exists; start cannot be attested")
		}
		waiting = waiting || c.State == "CONTAINER_RUNNING"
	}
	if !waiting {
		return errors.New("init gate is not holding workload")
	}
	return nil
}
func inspectEnded(ctx context.Context, r Request, id string) (runtimeContainer, error) {
	var v struct {
		Status runtimeContainer `json:"status"`
	}
	data, e := cri(ctx, "inspect", id)
	if e != nil {
		return v.Status, e
	}
	if e = json.Unmarshal(data, &v); e != nil {
		return v.Status, e
	}
	c := v.Status
	if c.ID != id || c.Metadata.Name != "guard" || c.Metadata.Attempt != 0 || c.Labels["io.kubernetes.pod.uid"] != r.UID || c.Labels["io.kubernetes.pod.namespace"] != r.Namespace() || c.State != "CONTAINER_EXITED" || c.LogPath != r.LogDirectory()+"/guard/0.log" {
		return c, errors.New("runtime termination or log binding mismatch")
	}
	return c, nil
}

// Collect is detached from the coordinator and writes only a private, per-run
// directory on the fixed node. Exclusive directory creation forbids re-arming.
func Collect(r Request) error {
	if e := r.Validate(); e != nil {
		return e
	}
	host, e := os.Hostname()
	if e != nil || host != Node || os.Geteuid() != 0 {
		return errors.New("collector requires the exact trusted kind node")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.Seconds)*time.Second)
	defer cancel()
	if e = os.Mkdir(r.Directory(), 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(r.Directory(), "lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return e
	}
	result := Result{Kind: "kind-node-observation/v1", Request: r, ArmedAt: time.Now().UTC(), Logs: Finding{"unobservable", "collector did not prove full coverage"}, Workspace: Finding{"unobservable", "guard termination not observed"}, Credentials: Finding{"unobservable", "guard termination not observed"}}
	spool, e := os.OpenFile(filepath.Join(r.Directory(), "job.log"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer spool.Close()
	collectErr := collect(ctx, r, &result, spool)
	if collectErr != nil {
		result.Logs = Finding{"unobservable", fmt.Sprintf("collector incomplete: %.800s", collectErr)}
	}
	if e = spool.Sync(); e != nil {
		return e
	}
	if _, e = spool.Seek(0, 0); e != nil {
		return e
	}
	result.LogsDigest, result.LogsBytes, e = digestReader(spool)
	if e != nil {
		return e
	}
	result.CompletedAt = time.Now().UTC()
	return writeJSON(r.Directory(), "result.json", result)
}
func collect(ctx context.Context, r Request, result *Result, spool *os.File) error {
	if e := checkBeforeStart(ctx, r); e != nil {
		return e
	}
	parent, e := openDirectory(r.LogDirectory())
	if e != nil {
		return e
	}
	defer parent.Close()
	// Creating just this empty runtime log directory closes the recursive-watch
	// installation race. No log file is created, altered, truncated, or deleted.
	if e = unix.Mkdirat(int(parent.Fd()), "guard", 0755); e != nil && !errors.Is(e, unix.EEXIST) {
		return e
	}
	guard, e := openChild(parent, "guard", unix.O_RDONLY|unix.O_DIRECTORY)
	if e != nil {
		return e
	}
	defer guard.Close()
	names, e := guard.Readdirnames(1)
	if len(names) > 0 || e != io.EOF {
		return errors.New("guard log directory was not empty before start")
	}
	watch, e := newLogWatch(parent, guard)
	if e != nil {
		return e
	}
	defer unix.Close(watch.fd)
	if e = checkBeforeStart(ctx, r); e != nil {
		return e
	}
	result.ArmedAt = time.Now().UTC()
	result.StartProven = true
	if e = writeJSON(r.Directory(), "armed.json", result); e != nil {
		return e
	}
	var raw *os.File
	defer func() {
		if raw != nil {
			raw.Close()
		}
	}()
	var copied int64
	pump := func() error {
		if e := watch.drain(); e != nil {
			return e
		}
		if raw == nil {
			f, e := openChild(guard, "0.log", unix.O_RDONLY)
			if errors.Is(e, unix.ENOENT) {
				return nil
			}
			if e != nil {
				return e
			}
			info, e := f.Stat()
			if e != nil || !info.Mode().IsRegular() {
				f.Close()
				return errors.New("CRI log is not a regular file")
			}
			raw = f
		}
		info, e := raw.Stat()
		if e != nil {
			return e
		}
		if info.Size() < copied {
			watch.problem = "CRI log truncated"
			return nil
		}
		n, e := io.Copy(spool, io.LimitReader(raw, MaxLogs-copied))
		copied += n
		if e != nil {
			return e
		}
		if info.Size() > MaxLogs {
			watch.problem = "100 MiB log limit exceeded"
		}
		return spool.Sync()
	}
	var lastID string
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e = pump(); e != nil {
			return e
		}
		cs, e := containers(ctx, r)
		if e != nil {
			return e
		}
		guards := 0
		allStopped := true
		var current runtimeContainer
		for _, c := range cs {
			if c.Metadata.Name == "guard" {
				guards++
				current = c
				if c.Metadata.Attempt != 0 {
					return errors.New("guard restarted")
				}
			}
			if c.State != "CONTAINER_EXITED" {
				allStopped = false
			}
			if c.Metadata.Name != "guard" && c.Metadata.Name != "network-gate" {
				return errors.New("unexpected container in runner sandbox")
			}
		}
		if guards > 1 {
			return errors.New("multiple guard containers")
		}
		if guards == 0 && lastID != "" {
			return errors.New("guard disappeared before terminal proof")
		}
		if guards == 1 {
			if lastID != "" && lastID != current.ID {
				return errors.New("guard container replaced")
			}
			lastID = current.ID
			if allStopped && current.State == "CONTAINER_EXITED" && (watch.closed || watch.problem != "") {
				ended, e := inspectEnded(ctx, r, current.ID)
				if e != nil {
					return e
				}
				start, e := runtimeTime(ended.StartedAt)
				if e != nil {
					return e
				}
				end, e := runtimeTime(ended.FinishedAt)
				if e != nil {
					return e
				}
				if start < result.ArmedAt.UnixNano() || end < start {
					return errors.New("runtime chronology does not prove observed start")
				}
				result.ContainerID = ended.ID
				result.ImageID = ended.ImageRef
				result.StartedAt = start
				result.FinishedAt = end
				result.ExitCode = ended.ExitCode
				result.TerminationProven = true
				// Both runtime exit and the observed writer CLOSE_WRITE are required;
				// a quiet polling interval alone is not proof of a drained log writer.
				if e = pump(); e != nil {
					return e
				}
				result.Workspace, result.Credentials = inspectWorkspace(r)
				if e = pump(); e != nil {
					return e
				}
				if raw == nil || !watch.created {
					return errors.New("initial CRI log creation not observed")
				}
				var stat unix.Stat_t
				if e = unix.Fstat(int(raw.Fd()), &stat); e != nil {
					return e
				}
				result.LogDevice = uint64(stat.Dev)
				result.LogInode = stat.Ino
				if _, e = raw.Seek(0, 0); e != nil {
					return e
				}
				sourceHash, n, e := digestReader(io.LimitReader(raw, MaxLogs+1))
				if e != nil {
					return e
				}
				if _, e = spool.Seek(0, 0); e != nil {
					return e
				}
				spoolHash, m, e := digestReader(spool)
				if e != nil {
					return e
				}
				if sourceHash != spoolHash || n != m || n > MaxLogs {
					watch.problem = "source/spool mismatch or truncation"
				}
				if e = watch.drain(); e != nil {
					return e
				}
				if watch.problem != "" {
					result.Logs = Finding{"unobservable", watch.problem}
				} else {
					result.Logs = Finding{"verified", "Exact CRI stdout/stderr bytes from watched initial file creation through runtime termination; no rotation or watch gap"}
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Read only exposes fixed filenames in a validated private run directory.
// Exit 75 means alive but not yet ready; 66 means irreversible missing result.
func Read(r Request, mode string, w io.Writer) error {
	if e := r.Validate(); e != nil {
		return e
	}
	dir, e := openDirectory(r.Directory())
	if e != nil {
		return e
	}
	defer dir.Close()
	if mode == "ready" || mode == "result" {
		lock, err := openChild(dir, "lock", unix.O_RDWR)
		if err != nil {
			return err
		}
		defer lock.Close()
		lockErr := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if lockErr != nil && !errors.Is(lockErr, unix.EWOULDBLOCK) {
			return lockErr
		}
		if mode == "ready" && lockErr == nil {
			return errors.New("collector is no longer alive; gate must remain closed")
		}
		if mode == "result" && lockErr == nil {
			// A dead collector cannot regain its observation window. Preserve its
			// bounded durable prefix and publish an explicit gap for finalization.
			if _, err := os.Lstat(filepath.Join(r.Directory(), "result.json")); errors.Is(err, os.ErrNotExist) {
				f, err := openChild(dir, "job.log", unix.O_RDONLY)
				if err != nil {
					return err
				}
				hash, size, err := digestReader(io.LimitReader(f, MaxLogs+1))
				f.Close()
				if err != nil || size > MaxLogs {
					return errors.New("interrupted spool invalid")
				}
				now := time.Now().UTC()
				gap := Finding{"unobservable", "Detached collector interrupted; durable prefix cannot attest start-through-termination coverage"}
				result := Result{Kind: "kind-node-observation/v1", Request: r, ArmedAt: now, CompletedAt: now, Logs: gap, Workspace: gap, Credentials: gap, LogsDigest: hash, LogsBytes: size}
				if err = writeJSON(r.Directory(), "result.json", result); err != nil {
					return err
				}
			}
		}
	}
	name := "result.json"
	limit := int64(64 << 10)
	if mode == "ready" {
		name = "armed.json"
	} else if mode == "logs" {
		name = "job.log"
		limit = MaxLogs
	} else if mode != "result" {
		return errors.New("invalid collector read mode")
	}
	f, e := openChild(dir, name, unix.O_RDONLY)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return errors.New("invalid collector artifact")
	}
	_, e = io.Copy(w, io.LimitReader(f, limit+1))
	return e
}
