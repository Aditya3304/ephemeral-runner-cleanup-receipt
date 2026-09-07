// Package watchdog reconciles local runs without trusting job-provided commands.
package watchdog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"golang.org/x/sys/unix"
)

type Report struct {
	Identity       proof.Identity `json:"identity"`
	Number         int            `json:"attempt_number"`
	Stage          string         `json:"stage"`
	Status         string         `json:"result"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ReceiptID      string         `json:"receipt_id,omitempty"`
	NextRetryAt    *time.Time     `json:"next_retry_at,omitempty"`
	LeaseOwner     string         `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time     `json:"lease_expires_at,omitempty"`
}
type State struct {
	Kind      string         `json:"kind"`
	Identity  proof.Identity `json:"identity"`
	Current   Report         `json:"current"`
	History   []Report       `json:"history"`
	UpdatedAt time.Time      `json:"updated_at"`
}
type Outcome struct {
	ReceiptID string
	Stage     string
	Permanent bool
	Exhausted bool
	NotBefore *time.Time
}

var ErrBusy = errors.New("another local worker is active")

type Backend interface {
	Reconcile(context.Context, proof.Identity) (string, error)
	Recover(context.Context, proof.Identity) (Outcome, error)
	Report(context.Context, Report) error
}
type Engine struct {
	Dir     string
	Backend Backend
	Now     func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
func Delay(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}
	return []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}[n-1]
}
func Key(id proof.Identity) string {
	id.Revision = ""
	b, _ := proof.Canonical(id)
	return proof.Digest(b)
}
func UUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
func Atomic(path string, v any) error {
	b, err := proof.Canonical(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
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
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (e *Engine) Lock() (func(), error) {
	if !filepath.IsAbs(e.Dir) {
		return nil, errors.New("absolute watchdog state directory required")
	}
	if err := os.MkdirAll(e.Dir, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(e.Dir)
	if err != nil || resolved != filepath.Clean(e.Dir) {
		return nil, errors.New("watchdog symlink forbidden")
	}
	f, err := os.OpenFile(filepath.Join(e.Dir, "worker.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		f.Close()
		return nil, ErrBusy
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func (e *Engine) Load(id proof.Identity) (*State, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if id.Provider != "local" {
		return nil, errors.New("local identity required")
	}
	s := &State{Kind: "local-watchdog-state/v1", Identity: id, Current: Report{Identity: id, Status: "pending", Stage: "collect"}, History: []Report{}}
	b, err := proof.ReadBounded(filepath.Join(e.Dir, Key(id)+".json"), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, s) != nil || s.Kind != "local-watchdog-state/v1" || s.Identity != id || s.Current.Identity != id || s.Current.Number < 0 || s.Current.Number > 6 || len(s.History) > 6 {
		return nil, errors.New("invalid watchdog state")
	}
	canonical, _ := proof.Canonical(s)
	if string(b) != string(canonical) {
		return nil, errors.New("noncanonical watchdog state")
	}
	if !validState(s) {
		return nil, errors.New("inconsistent watchdog state")
	}
	return s, nil
}
func validState(s *State) bool {
	uuid := regexp.MustCompile("^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$")
	r := s.Current
	if s.UpdatedAt.IsZero() || r.Number > 0 && len(s.History) != r.Number-1 {
		return false
	}
	for i, h := range s.History {
		if h.Identity != s.Identity || h.Number != i+1 || h.Status != "retry" || h.NextRetryAt == nil || h.LeaseOwner != "" || h.LeaseExpiresAt != nil {
			return false
		}
	}
	if r.Status == "succeeded" {
		return r.Stage == "complete" && uuid.MatchString(r.ReceiptID) && r.ErrorCode == "" && r.NextRetryAt == nil && r.LeaseOwner == "" && r.LeaseExpiresAt == nil
	}
	if r.ReceiptID != "" || (r.Stage != "collect" && r.Stage != "archive" && r.Stage != "sign" && r.Stage != "ingest") {
		return false
	}
	switch r.Status {
	case "pending":
		return r.Number == 0 && len(s.History) == 0 && r.NextRetryAt == nil && r.LeaseOwner == "" && r.LeaseExpiresAt == nil
	case "running":
		return r.Number > 0 && uuid.MatchString(r.LeaseOwner) && r.LeaseExpiresAt != nil && r.NextRetryAt != nil && r.ErrorCode == ""
	case "retry", "exhausted":
		return r.Number > 0 && r.ErrorCode != "" && r.LeaseOwner == "" && r.LeaseExpiresAt == nil && ((r.Status == "retry" && r.Number < 6 && r.NextRetryAt != nil) || (r.Status == "exhausted" && r.NextRetryAt == nil))
	}
	return false
}
func (e *Engine) save(s *State) error {
	s.UpdatedAt = e.now()
	return Atomic(filepath.Join(e.Dir, Key(s.Identity)+".json"), s)
}
func (e *Engine) flush(ctx context.Context, s *State) {
	for _, r := range s.History {
		if e.Backend.Report(ctx, r) != nil {
			return
		}
	}
	if s.Current.Number > 0 {
		_ = e.Backend.Report(ctx, s.Current)
	}
}

// Step requires the process flock. The durable lease describes ownership; an
// expired timestamp never steals work from a live process holding that lock.
func (e *Engine) Step(ctx context.Context, id proof.Identity) (*State, error) {
	s, err := e.Load(id)
	if err != nil {
		return nil, err
	}
	e.flush(ctx, s)
	if s.Current.Status == "succeeded" {
		return s, nil
	}
	if receipt, lookupErr := e.Backend.Reconcile(ctx, id); lookupErr == nil && receipt != "" {
		s.Current.Status = "succeeded"
		s.Current.Stage = "complete"
		s.Current.ReceiptID = receipt
		s.Current.NextRetryAt = nil
		s.Current.ErrorCode = ""
		s.Current.LeaseOwner = ""
		s.Current.LeaseExpiresAt = nil
		if err = e.save(s); err != nil {
			return nil, err
		}
		e.flush(ctx, s)
		return s, nil
	}
	if s.Current.Status == "running" {
		s.Current.Status = "retry"
		s.Current.ErrorCode = "worker_interrupted"
		s.Current.LeaseOwner = ""
		s.Current.LeaseExpiresAt = nil
		if s.Current.Number >= 6 {
			s.Current.Status = "exhausted"
			s.Current.NextRetryAt = nil
		}
		if err = e.save(s); err != nil {
			return nil, err
		}
		e.flush(ctx, s)
	}
	if s.Current.Status == "exhausted" || s.Current.Number >= 6 {
		return s, nil
	}
	if s.Current.NextRetryAt != nil && e.now().Before(*s.Current.NextRetryAt) {
		return s, nil
	}
	previous := s.Current
	if previous.Number > 0 {
		s.History = append(s.History, previous)
	}
	due := e.now().Add(Delay(previous.Number + 1))
	lease := e.now().Add(10 * time.Minute)
	s.Current = Report{Identity: id, Number: previous.Number + 1, Stage: "collect", Status: "running", NextRetryAt: &due, LeaseOwner: UUID(), LeaseExpiresAt: &lease}
	if err = e.save(s); err != nil {
		return nil, err
	}
	// Retry time is local crash intent; a running API row has no retry timestamp.
	running := s.Current
	running.NextRetryAt = nil
	_ = e.Backend.Report(ctx, running)
	result, runErr := e.Backend.Recover(ctx, id)
	if errors.Is(runErr, ErrBusy) {
		s.Current = previous
		if previous.Number > 0 {
			s.History = s.History[:len(s.History)-1]
		}
		return s, e.save(s)
	}
	s.Current.LeaseOwner = ""
	s.Current.LeaseExpiresAt = nil
	if result.ReceiptID != "" && runErr == nil {
		s.Current.Status = "succeeded"
		s.Current.Stage = "complete"
		s.Current.ReceiptID = result.ReceiptID
		s.Current.NextRetryAt = nil
		s.Current.ErrorCode = ""
	} else {
		s.Current.Status = "retry"
		s.Current.ErrorCode = "dependency_unavailable"
		s.Current.Stage = result.Stage
		if s.Current.Stage == "" {
			s.Current.Stage = "collect"
		}
		if result.Permanent {
			s.Current.ErrorCode = "invalid_evidence"
		}
		if result.Exhausted {
			s.Current.ErrorCode = "stage_exhausted"
		}
		if result.Permanent || result.Exhausted || s.Current.Number >= 6 {
			s.Current.Status = "exhausted"
			s.Current.NextRetryAt = nil
		} else {
			next := e.now().Add(Delay(s.Current.Number))
			if result.NotBefore != nil && result.NotBefore.After(next) {
				next = *result.NotBefore
			}
			s.Current.NextRetryAt = &next
		}
	}
	if err = e.save(s); err != nil {
		return nil, err
	}
	e.flush(ctx, s)
	return s, runErr
}
