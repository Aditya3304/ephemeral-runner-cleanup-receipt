package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubrun"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Root            string            `json:"root"`
	State           string            `json:"state"`
	FinalizerImage  string            `json:"finalizer_image"`
	APIImage        string            `json:"api_image"`
	FinalizerConfig string            `json:"finalizer_config"`
	Files           map[string]string `json:"files"`
	GitHub          *githubrun.Config `json:"github,omitempty"`
}
type backend struct {
	cfg        config
	c          *coordinator.Coordinator
	snapshot   watchdog.Inspection
	inspectErr error
	github     *githubrun.Client
}

func (b *backend) inputRoot() string {
	if b.github != nil {
		return filepath.Join(b.cfg.Root, ".build/github")
	}
	return b.c.Root
}

func (b *backend) command(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	name := "proof-watchdog-" + watchdog.UUID()
	if len(args) > 0 && args[0] == "run" {
		args = append([]string{"run", "--name", name, "--label", "cleanup-receipt.watchdog=operator"}, args[1:]...)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", args...)
	cmd.Cancel = func() error {
		clean, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(clean, "/usr/bin/docker", "rm", "--force", name).Run()
		return cmd.Process.Kill()
	}
	cmd.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	e := cmd.Run()
	if output.Len() > 2<<20 {
		return nil, errors.New("bounded output exceeded")
	}
	return output.Bytes(), e
}
func (b *backend) bridge(ctx context.Context, mode, key string, input []byte) ([]byte, error) {
	args := []string{"run", "--rm", "--pull=never", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", "65532:65532", "--memory", "128m", "--pids-limit", "32", "--network", "cleanup-receipt-api_delivery"}
	for _, v := range []string{"cleanup-receipt-api_auth:/run/auth:ro", "cleanup-receipt-finalizer_state:/finalizer:ro", "cleanup-receipt-finalizer_output:/input:ro", "cleanup-receipt-api_delivery:/delivery:ro"} {
		args = append(args, "-v", v)
	}
	if input != nil {
		args = append(args, "-i")
	}
	args = append(args, b.cfg.APIImage, "/usr/local/bin/recoverybridge", mode)
	if key != "" {
		args = append(args, key)
	}
	return b.command(ctx, input, args...)
}
func (b *backend) inspect(ctx context.Context, id proof.Identity) (watchdog.Inspection, error) {
	out := watchdog.Inspection{}
	data, e := b.bridge(ctx, "inspect", watchdog.Key(id), nil)
	if e == nil {
		e = json.Unmarshal(data, &out)
	}
	return out, e
}
func (b *backend) Reconcile(ctx context.Context, id proof.Identity) (string, error) {
	s, e := b.inspect(ctx, id)
	b.snapshot = s
	b.inspectErr = e
	return s.ReceiptID, e
}
func (b *backend) Report(ctx context.Context, r watchdog.Report) error {
	if r.Status == "running" {
		r.NextRetryAt = nil
	}
	data, _ := json.Marshal(r)
	_, e := b.bridge(ctx, "report", "", data)
	return e
}
func (b *backend) Recover(ctx context.Context, id proof.Identity) (watchdog.Outcome, error) {
	fail := func(stage string, e error) (watchdog.Outcome, error) { return watchdog.Outcome{Stage: stage}, e }
	inputKey := id.Run
	if b.github != nil {
		inputKey = githubrun.Directory(id)
		if _, err := githubrun.Load(b.inputRoot(), id); errors.Is(err, os.ErrNotExist) {
			l, files, err := b.github.Collect(ctx, id)
			if errors.Is(err, githubrun.ErrPending) {
				return fail("collect", watchdog.ErrBusy)
			}
			if err != nil {
				return fail("collect", err)
			}
			if err = githubrun.Publish(b.inputRoot(), l, files); err != nil {
				return fail("collect", err)
			}
		} else if err != nil {
			return watchdog.Outcome{Stage: "collect", Permanent: true}, err
		}
	} else {
		// Shared coordinator flock is held by cycle. Never run a ledger command.
		l, e := b.c.Load(id.Run)
		if e != nil || l.Identity != id {
			return watchdog.Outcome{Stage: "collect", Permanent: true}, errors.New("ledger identity changed")
		}
		ready := filepath.Join(b.c.Root, id.Run, "finalization-ready.json")
		if _, e = os.Stat(ready); e != nil || l.Phase != "complete" {
			// Recovery must not launch a job that never started. Record cancellation
			// before the coordinator resumes its existing UID-bound collection path.
			if l.JobUID == "" && !l.LaunchAttempted {
				if e = b.c.RequestCancel(id.Run); e != nil {
					return fail("collect", e)
				}
			}
			if _, e = b.c.Collect(ctx, id.Run); e != nil {
				return fail("collect", e)
			}
		}
		if e = validateInputs(filepath.Join(b.c.Root, id.Run), id); e != nil {
			return watchdog.Outcome{Stage: "collect", Permanent: errors.Is(e, errInvalidInputs)}, e
		}
	}
	state, e := b.snapshot, b.inspectErr
	if e != nil {
		return fail("archive", e)
	}
	if !state.Published {
		if state.FinalizerStatus == "exhausted" {
			return watchdog.Outcome{Stage: "sign", Exhausted: true}, errors.New("finalizer exhausted")
		}
		if state.FinalizerDue != nil && time.Now().Before(*state.FinalizerDue) {
			return watchdog.Outcome{Stage: "sign", NotBefore: state.FinalizerDue}, errors.New("finalizer retry pending")
		}
		args := []string{"run", "--rm", "--pull=never", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", "65532:65532", "--memory", "768m", "--cpus", "1", "--pids-limit", "64", "--tmpfs", "/tmp:rw,noexec,nosuid,size=384m,uid=65532,gid=65532,mode=700", "--network", "cleanup-receipt-archive", "--network", "cleanup-receipt-sigstore_sigstore", "-e", "GOMEMLIMIT=512MiB", "-e", "GOMAXPROCS=2"}
		for _, v := range []string{b.inputRoot() + ":/input:ro", b.cfg.FinalizerConfig + ":/config/finalizer.json:ro", "cleanup-receipt-archive_finalizer:/secrets/archive:ro", "cleanup-receipt-sigstore_finalizer-identity:/secrets/signing:ro", "cleanup-receipt-sigstore_sigstore-trust:/trust:ro", "cleanup-receipt-finalizer_state:/state", "cleanup-receipt-finalizer_output:/output"} {
			args = append(args, "-v", v)
		}
		args = append(args, b.cfg.FinalizerImage, "--config", "/config/finalizer.json", "--run", inputKey)
		if _, e = b.command(ctx, nil, args...); e != nil {
			after, _ := b.inspect(ctx, id)
			stage := after.FinalizerStage
			if stage != "archive" && stage != "sign" {
				stage = "sign"
			}
			return watchdog.Outcome{Stage: stage, NotBefore: after.FinalizerDue, Exhausted: after.FinalizerStatus == "exhausted"}, e
		}
	}
	args := []string{"run", "--rm", "--pull=never", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", "65532:65532", "--memory", "128m", "--pids-limit", "32", "--network", "cleanup-receipt-api_delivery",
		"-v", "cleanup-receipt-api_auth:/run/auth:ro", "-v", "cleanup-receipt-api_delivery:/delivery", "-v", "cleanup-receipt-finalizer_output:/input:ro", b.cfg.APIImage, "/usr/local/bin/deliver", "/input/" + watchdog.Key(id) + "/result.json"}
	_, deliverErr := b.command(ctx, nil, args...)
	after, e := b.inspect(ctx, id)
	if e == nil && after.ReceiptID != "" {
		return watchdog.Outcome{ReceiptID: after.ReceiptID}, nil
	}
	if deliverErr == nil {
		deliverErr = errors.New("verified receipt not yet visible")
	}
	return watchdog.Outcome{Stage: "ingest", NotBefore: after.DeliveryDue, Exhausted: after.DeliveryStatus == "exhausted"}, deliverErr
}
func validate(c config) error {
	if c.GitHub != nil && c.GitHub.Validate() != nil {
		return errors.New("invalid GitHub policy")
	}
	if !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.State) || !filepath.IsAbs(c.FinalizerConfig) || len(c.Files) < 2 {
		return errors.New("invalid operator configuration")
	}
	for _, s := range []string{c.APIImage, c.FinalizerImage} {
		if !regexp.MustCompile("^sha256:[a-f0-9]{64}$").MatchString(s) {
			return errors.New("image must be pinned")
		}
	}
	for path, digest := range c.Files {
		b, e := proof.ReadBounded(path, 100<<20)
		if e != nil || proof.Digest(b) != digest {
			return errors.New("installed operator artifact changed")
		}
	}
	if c.Files[c.FinalizerConfig] == "" {
		return errors.New("finalizer config must be pinned")
	}
	return nil
}
func cycle(ctx context.Context, cfg config, only string) error {
	if e := validate(cfg); e != nil {
		return e
	}
	// Both profiles share signer/delivery volumes and child labels. A single
	// process lock prevents one profile from deleting another's live children.
	engine := watchdog.Engine{Dir: cfg.State}
	unlock, e := engine.Lock()
	if e != nil {
		return e
	}
	defer unlock()
	b := &backend{cfg: cfg}
	engine.Backend = b
	if cfg.GitHub != nil {
		b.github, e = githubrun.New(*cfg.GitHub)
		if e != nil {
			return e
		}
	} else {
		b.c, e = coordinator.Connect(filepath.Join(cfg.Root, ".build/kubeconfig"), filepath.Join(cfg.Root, ".build/coordinator"))
		if e != nil {
			return e
		}
		b.c.Out = io.Discard
		release, err := b.c.Lock()
		if err != nil {
			return watchdog.ErrBusy
		}
		defer release()
	}
	// A process killed with SIGKILL may leave its isolated Docker child alive.
	// Only this project's explicitly labelled watchdog children are eligible.
	orphans, e := exec.CommandContext(ctx, "/usr/bin/docker", "ps", "-aq", "--filter", "label=cleanup-receipt.watchdog=operator").Output()
	if e != nil {
		return e
	}
	for _, id := range strings.Fields(string(orphans)) {
		if !regexp.MustCompile("^[a-f0-9]{12,64}$").MatchString(id) {
			return errors.New("invalid orphan identity")
		}
		if e = exec.CommandContext(ctx, "/usr/bin/docker", "rm", "--force", id).Run(); e != nil {
			return e
		}
	}
	ids := []proof.Identity{}
	if b.github != nil {
		ids, e = b.github.Discover(ctx)
		if e != nil {
			fmt.Fprintln(os.Stderr, "GitHub discovery unavailable; retrying durable backlog:", e)
		}
		// Persisted identities survive an unavailable API and discovery-window
		// changes. Step can still deliver an already signed receipt offline.
		entries, err := os.ReadDir(cfg.State)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range ids {
			seen[watchdog.Key(id)] = true
		}
		for _, entry := range entries {
			if entry.IsDir() || !regexp.MustCompile(`^[a-f0-9]{64}\.json$`).MatchString(entry.Name()) {
				continue
			}
			data, err := proof.ReadBounded(filepath.Join(cfg.State, entry.Name()), 64<<10)
			var s watchdog.State
			if err != nil || json.Unmarshal(data, &s) != nil || s.Identity.Provider != "github" || s.Identity.Repository != cfg.GitHub.Repository || cfg.GitHub.Jobs[s.Identity.Job] == "" || seen[watchdog.Key(s.Identity)] {
				continue
			}
			if _, err = engine.Load(s.Identity); err != nil {
				return err
			}
			ids = append(ids, s.Identity)
			seen[watchdog.Key(s.Identity)] = true
		}
	} else {
		entries, e := os.ReadDir(b.c.Root)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if !entry.IsDir() || !regexp.MustCompile("^[0-9a-f]{32}$").MatchString(entry.Name()) || (only != "" && entry.Name() != only) {
				continue
			}
			l, e := b.c.Load(entry.Name())
			if e != nil {
				fmt.Println("Skipped invalid coordinator ledger:", entry.Name())
				continue
			}
			if time.Since(l.CreatedAt) < time.Duration(l.TimeoutSeconds)*time.Second+time.Minute && l.Phase != "complete" {
				continue
			}
			ids = append(ids, l.Identity)
		}
	}
	discoveryErr := e
	sort.Slice(ids, func(i, j int) bool { return ids[i].Run < ids[j].Run })
	for _, id := range ids {
		if only != "" && id.Run != only {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		runctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		s, e := engine.Step(runctx, id)
		cancel()
		if s != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"provider": id.Provider, "run_id": id.Run, "run_attempt": id.Attempt, "job_id": id.Job, "status": s.Current.Status, "attempts": s.Current.Number, "stage": s.Current.Stage, "next_retry_at": s.Current.NextRetryAt, "receipt_id": s.Current.ReceiptID})
		}
		if e != nil && s == nil {
			fmt.Println("Recovery state unavailable:", id.Run)
		}
	}
	return discoveryErr
}
func main() {
	path := flag.String("config", "", "Installed operator config")
	once := flag.Bool("once", false, "One reconciliation cycle")
	only := flag.String("run", "", "Restrict a manual cycle to a provider run ID")
	flag.Parse()
	if *only != "" && (!*once || !regexp.MustCompile("^([a-f0-9]{32}|[1-9][0-9]{0,18})$").MatchString(*only)) {
		os.Exit(2)
	}
	data, e := proof.ReadBounded(*path, 64<<10)
	var cfg config
	if e != nil || json.Unmarshal(data, &cfg) != nil || validate(cfg) != nil {
		fmt.Fprintln(os.Stderr, "Invalid watchdog installation")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		if e = cycle(ctx, cfg, *only); e != nil && !errors.Is(e, watchdog.ErrBusy) && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "Watchdog cycle unavailable; durable state retained")
			if *once {
				os.Exit(1)
			}
		}
		if *once || ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(5 * time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
