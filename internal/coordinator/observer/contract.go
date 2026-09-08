// Package observer implements the trusted, fixed-kind-node collection boundary.
// Its output is unsigned host evidence, never a job upload or a signed receipt.
package observer

import (
	"errors"
	"regexp"
	"time"
)

const Node = "cleanup-receipt-control-plane"
const MaxLogs int64 = 100 << 20

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var podName = regexp.MustCompile(`^job-[a-z0-9]{5}$`)

// ErrResultLost means the detached collector's private state disappeared after
// it was armed. The observation window cannot be recreated or retried.
var ErrResultLost = errors.New("collector result irreversibly unavailable")

type Request struct {
	GitHub  bool   `json:"github,omitempty"`
	Run     string `json:"run"`
	Pod     string `json:"pod"`
	UID     string `json:"pod_uid"`
	Sandbox string `json:"sandbox_id"`
	Binding string `json:"binding_sha256"`
	Seconds int    `json:"seconds"`
}

func (r Request) Validate() error {
	if !hex32.MatchString(r.Run) || !uid.MatchString(r.UID) || !podName.MatchString(r.Pod) || !hex64.MatchString(r.Sandbox) || !hex64.MatchString(r.Binding) || r.Seconds < 1 || r.Seconds > 900 {
		return errors.New("invalid pinned collector request")
	}
	return nil
}
func (r Request) Namespace() string { return "proof-runner-" + r.Run }
func (r Request) Directory() string { return "/tmp/proof-observe-" + r.Run + "-" + r.UID }
func (r Request) LogDirectory() string {
	return "/var/log/pods/" + r.Namespace() + "_" + r.Pod + "_" + r.UID
}

type Finding struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type Result struct {
	Kind              string    `json:"kind"`
	Request           Request   `json:"request"`
	ArmedAt           time.Time `json:"armed_at"`
	CompletedAt       time.Time `json:"completed_at"`
	StartProven       bool      `json:"start_proven"`
	TerminationProven bool      `json:"termination_proven"`
	ContainerID       string    `json:"container_id"`
	ImageID           string    `json:"image_id"`
	StartedAt         int64     `json:"started_at_ns,string"`
	FinishedAt        int64     `json:"finished_at_ns,string"`
	ExitCode          int32     `json:"exit_code"`
	LogDevice         uint64    `json:"log_device,string"`
	LogInode          uint64    `json:"log_inode,string"`
	Logs              Finding   `json:"logs"`
	LogsDigest        string    `json:"logs_sha256"`
	LogsBytes         int64     `json:"logs_bytes"`
	Workspace         Finding   `json:"workspace"`
	Credentials       Finding   `json:"credentials"`
}

func (r *Result) Validate() error {
	if err := r.Request.Validate(); err != nil {
		return err
	}
	if r.Kind != "kind-node-observation/v1" || r.ArmedAt.IsZero() || r.CompletedAt.Before(r.ArmedAt) || r.LogsBytes < 0 || r.LogsBytes > MaxLogs || !hex64.MatchString(r.LogsDigest) {
		return errors.New("invalid collector result")
	}
	for _, f := range []Finding{r.Logs, r.Workspace, r.Credentials} {
		if (f.Status != "verified" && f.Status != "unobservable" && f.Status != "failed") || len(f.Reason) == 0 || len(f.Reason) > 1024 {
			return errors.New("invalid collector finding")
		}
	}
	if r.TerminationProven && (!hex64.MatchString(r.ContainerID) || r.ImageID == "" || r.StartedAt <= 0 || r.FinishedAt < r.StartedAt) {
		return errors.New("invalid runtime termination proof")
	}
	if r.Logs.Status == "verified" && (!r.StartProven || !r.TerminationProven || r.ArmedAt.UnixNano() > r.StartedAt || r.LogInode == 0) {
		return errors.New("complete logs require proven start and termination")
	}
	if (r.Workspace.Status != "unobservable" || r.Credentials.Status != "unobservable") && !r.TerminationProven {
		return errors.New("filesystem finding without stopped container")
	}
	return nil
}
