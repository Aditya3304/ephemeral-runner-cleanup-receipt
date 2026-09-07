package watchdog

import (
	"context"
	"errors"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fake struct {
	calls     int
	receipt   string
	err       error
	permanent bool
	reports   []Report
	reportErr error
}

func (f *fake) Reconcile(context.Context, proof.Identity) (string, error) { return f.receipt, nil }
func (f *fake) Recover(context.Context, proof.Identity) (Outcome, error) {
	f.calls++
	return Outcome{Stage: "sign", Permanent: f.permanent}, f.err
}
func (f *fake) Report(_ context.Context, r Report) error {
	f.reports = append(f.reports, r)
	return f.reportErr
}
func identity() proof.Identity {
	return proof.Identity{Provider: "local", Repository: "Aditya3304/ephemeral-runner-cleanup-receipt", Run: strings.Repeat("a", 32), Attempt: 1, Job: "watchdog", Revision: strings.Repeat("b", 40)}
}
func setup(t *testing.T) (*Engine, *fake, *time.Time) {
	t.Helper()
	now := time.Now().UTC()
	f := &fake{err: errors.New("offline")}
	e := &Engine{Dir: t.TempDir(), Backend: f, Now: func() time.Time { return now }}
	release, err := e.Lock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return e, f, &now
}
func TestRetryBudgetRestartAndExhaustion(t *testing.T) {
	e, b, now := setup(t)
	id := identity()
	for n := 1; n <= 6; n++ {
		s, _ := e.Step(context.Background(), id)
		if s.Current.Number != n || b.calls != n {
			t.Fatalf("budget %d: %+v", n, s)
		}
		if n < 6 {
			if !s.Current.NextRetryAt.Equal(now.Add(Delay(n))) {
				t.Fatal("wrong backoff")
			}
			again, _ := e.Step(context.Background(), id)
			if b.calls != n || again.Current.Number != n {
				t.Fatal("early retry")
			}
			*now = *s.Current.NextRetryAt
			e = &Engine{Dir: e.Dir, Backend: b, Now: func() time.Time { return *now }}
		} else if s.Current.Status != "exhausted" || s.Current.NextRetryAt != nil {
			t.Fatal("not exhausted")
		}
	}
	*now = now.Add(24 * time.Hour)
	_, _ = e.Step(context.Background(), id)
	if b.calls != 6 {
		t.Fatal("exhaustion bypassed")
	}
	b.receipt = UUID()
	s, err := e.Step(context.Background(), id)
	if err != nil || s.Current.Status != "succeeded" || b.calls != 6 {
		t.Fatal("lost successful commit was not reconciled")
	}
}
func TestCrashConsumesAttemptAndPersistsReports(t *testing.T) {
	e, b, now := setup(t)
	id := identity()
	s, _ := e.Load(id)
	due := now.Add(time.Minute)
	lease := now.Add(10 * time.Minute)
	s.Current = Report{Identity: id, Number: 1, Status: "running", Stage: "sign", NextRetryAt: &due, LeaseOwner: UUID(), LeaseExpiresAt: &lease}
	if err := e.save(s); err != nil {
		t.Fatal(err)
	}
	b.reportErr = errors.New("db down")
	s, _ = e.Step(context.Background(), id)
	if s.Current.Status != "retry" || s.Current.ErrorCode != "worker_interrupted" || b.calls != 0 {
		t.Fatal("interrupted attempt reset or retried early")
	}
	b.reportErr = nil
	*now = due
	s, _ = e.Step(context.Background(), id)
	if s.Current.Number != 2 || len(s.History) != 1 {
		t.Fatal("interruption history lost")
	}
	found := false
	for _, r := range b.reports {
		if r.ErrorCode == "worker_interrupted" {
			found = true
		}
	}
	if !found {
		t.Fatal("durable report not replayed")
	}
}
func TestPermanentFailureAndSuccessfulReconciliation(t *testing.T) {
	e, b, _ := setup(t)
	b.permanent = true
	s, _ := e.Step(context.Background(), identity())
	if s.Current.Status != "exhausted" || s.Current.Number != 1 {
		t.Fatal("invalid evidence should not loop")
	}
	b.receipt = UUID()
	s, err := e.Step(context.Background(), identity())
	if err != nil || s.Current.Status != "succeeded" {
		t.Fatal("independent receipt reconciliation failed")
	}
}
func TestLockAndStateSafety(t *testing.T) {
	e, _, _ := setup(t)
	if release, err := e.Lock(); err == nil {
		release()
		t.Fatal("overlapping worker admitted")
	}
	id := identity()
	path := filepath.Join(e.Dir, Key(id)+".json")
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Step(context.Background(), id); err == nil {
		t.Fatal("corrupt state accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	_ = os.WriteFile(other, []byte("{}"), 0600)
	_ = os.Symlink(other, path)
	if _, err := e.Load(id); err == nil {
		t.Fatal("symlink state accepted")
	}
}
