// Package coordinator owns the local CI ledger and the Kubernetes runner lifecycle.
// No data read from a runner can authorize privileged resource deletion.
package coordinator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator/observer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"golang.org/x/sys/unix"
)

type Ledger struct {
	Kind              string            `json:"kind"`
	Identity          proof.Identity    `json:"identity"`
	Token             string            `json:"token"`
	ClusterUID        string            `json:"cluster_uid"`
	ResourceNamespace string            `json:"resource_namespace"`
	ResourceUID       string            `json:"resource_uid"`
	RunnerNamespace   string            `json:"runner_namespace"`
	RunnerUID         string            `json:"runner_uid"`
	JobUID            string            `json:"job_uid"`
	LaunchAttempted   bool              `json:"launch_attempted"`
	PodName           string            `json:"pod_name"`
	PodUID            string            `json:"pod_uid"`
	StageUID          string            `json:"stage_uid"`
	RoleUID           string            `json:"role_uid"`
	BindingUID        string            `json:"binding_uid"`
	Image             string            `json:"image"`
	ActualImage       string            `json:"actual_image"`
	Command           []string          `json:"command"`
	TimeoutSeconds    int64             `json:"timeout_seconds"`
	Phase             string            `json:"phase"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	CancelRequested   bool              `json:"cancel_requested"`
	PodExitCode       *int32            `json:"pod_exit_code"`
	PodReason         string            `json:"pod_reason"`
	EvidenceStatus    string            `json:"evidence_status"`
	EvidenceDigest    string            `json:"evidence_sha256"`
	GuardDigest       string            `json:"guard_sha256"`
	LogsStatus        string            `json:"logs_status"`
	LogsDigest        string            `json:"logs_sha256"`
	LogsBytes         int64             `json:"logs_bytes"`
	ResourceAbsent    bool              `json:"resource_absent"`
	RunnerAbsent      bool              `json:"runner_absent"`
	RoleAbsent        bool              `json:"role_absent"`
	BindingAbsent     bool              `json:"binding_absent"`
	ContainerID       string            `json:"container_id"`
	CollectorRequest  *observer.Request `json:"collector_request,omitempty"`
	CollectorDigest   string            `json:"collector_sha256,omitempty"`
	Errors            []string          `json:"errors"`
}

var runPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (c *Coordinator) runDir(run string) (string, error) {
	if !runPattern.MatchString(run) {
		return "", errors.New("invalid local run ID")
	}
	return filepath.Join(c.Root, run), nil
}

func (c *Coordinator) Load(run string) (*Ledger, error) {
	dir, err := c.runDir(run)
	if err != nil {
		return nil, err
	}
	data, err := proof.ReadBounded(filepath.Join(dir, "run.json"), 1<<20)
	if err != nil {
		return nil, err
	}
	var l Ledger
	if err = strict(data, &l); err != nil {
		return nil, err
	}
	if l.Kind != "local-ci-run/v1" || l.Identity.Run != run || l.Token != run || l.ResourceNamespace != "proof-"+run || l.RunnerNamespace != "proof-runner-"+run {
		return nil, errors.New("invalid ledger identity")
	}
	if err = l.Identity.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func strict(data []byte, v any) error {
	if _, err := jcs.Transform(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func durable(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ledgerBytes(l *Ledger) ([]byte, error) {
	l.UpdatedAt = time.Now().UTC()
	data, err := proof.Canonical(l)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("ledger exceeds 1 MiB limit")
	}
	return data, nil
}

// publish makes the initial intent visible only after its contents are durable.
// A crash before rename leaves an ignored .new-* directory, never an empty run.
func (c *Coordinator) publish(l *Ledger) error {
	dir, err := c.runDir(l.Identity.Run)
	if err != nil {
		return err
	}
	data, err := ledgerBytes(l)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(c.Root, ".new-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err = durable(filepath.Join(tmp, "run.json"), data); err != nil {
		return err
	}
	if err = os.Rename(tmp, dir); err != nil {
		return err
	}
	parent, err := os.Open(c.Root)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func (c *Coordinator) save(l *Ledger) error {
	data, err := ledgerBytes(l)
	if err != nil {
		return err
	}
	dir, err := c.runDir(l.Identity.Run)
	if err != nil {
		return err
	}
	return durable(filepath.Join(dir, "run.json"), data)
}

// The coordinator is deliberately single-run on this laptop. A process death
// releases flock, while the durable intent remains available to collect/reconcile.
func (c *Coordinator) Lock() (func(), error) {
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.Root, "coordinator.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another coordinator is active; use cancel or inspect")
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}

func (c *Coordinator) RequestCancel(run string) error {
	if _, err := c.Load(run); err != nil {
		return err
	}
	dir, _ := c.runDir(run)
	return durable(filepath.Join(dir, "cancel.requested"), []byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func (c *Coordinator) problem(l *Ledger, code string, err error) {
	if len(l.Errors) >= 64 {
		return
	}
	message := code
	if err != nil {
		message = fmt.Sprintf("%s: %.300s", code, err.Error())
	}
	for _, old := range l.Errors {
		if old == message {
			return
		}
	}
	l.Errors = append(l.Errors, message)
}
