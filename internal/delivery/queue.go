// Package delivery persists delivery independently of PostgreSQL availability.
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/api"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"golang.org/x/sys/unix"
)

type State struct {
	Result      finalizer.Result `json:"result"`
	Tries       int              `json:"tries"`
	Status      string           `json:"status"`
	ReceiptID   string           `json:"receipt_id"`
	NextRetryAt *time.Time       `json:"next_retry_at"`
	Failures    []api.Attempt    `json:"failures"`
}
type Queue struct {
	Dir, BaseURL, Token string
	Client              *http.Client
	Now                 func() time.Time
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now().UTC()
	}
	return time.Now().UTC()
}
func save(path string, s *State) error {
	b, e := proof.Canonical(s)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (q *Queue) request(ctx context.Context, method, path string, data []byte) (int, []byte, error) {
	req, e := http.NewRequestWithContext(ctx, method, q.BaseURL+path, bytes.NewReader(data))
	if e != nil {
		return 0, nil, e
	}
	req.Header.Set("Authorization", "Bearer "+q.Token)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := q.Client.Do(req)
	if e != nil {
		return 0, nil, e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if len(b) > 1<<20 {
		return 0, nil, errors.New("response too large")
	}
	return resp.StatusCode, b, e
}
func (q *Queue) reconcile(ctx context.Context, s *State) bool {
	status, b, e := q.request(ctx, "GET", "/v1/receipts?run_id="+s.Result.Identity.Run+"&limit=100", nil)
	if e != nil || status != 200 {
		return false
	}
	var page struct {
		Items []struct {
			ID      string          `json:"id"`
			Receipt json.RawMessage `json:"receipt_object"`
			Bundle  json.RawMessage `json:"bundle_object"`
		} `json:"items"`
	}
	if json.Unmarshal(b, &page) != nil {
		return false
	}
	for _, row := range page.Items {
		var r finalizer.Result
		if json.Unmarshal(row.Receipt, &r.Receipt) == nil && json.Unmarshal(row.Bundle, &r.Bundle) == nil && r.Receipt == s.Result.Receipt && r.Bundle == s.Result.Bundle {
			s.ReceiptID = row.ID
			s.Status = "complete"
			s.NextRetryAt = nil
			return true
		}
	}
	return false
}
func (q *Queue) flush(ctx context.Context, s *State) bool {
	for _, a := range s.Failures {
		b, _ := json.Marshal(a)
		status, _, e := q.request(ctx, "POST", "/v1/attempts", b)
		if e != nil || (status != 200 && status != 201) {
			return false
		}
	}
	return true
}
func (q *Queue) Deliver(ctx context.Context, data []byte) (*State, error) {
	r, e := finalizer.ParseResult(data)
	if e != nil {
		return nil, e
	}
	if !filepath.IsAbs(q.Dir) {
		return nil, errors.New("absolute queue directory required")
	}
	if e = os.MkdirAll(q.Dir, 0700); e != nil {
		return nil, e
	}
	resolved, e := filepath.EvalSymlinks(q.Dir)
	if e != nil || resolved != filepath.Clean(q.Dir) {
		return nil, errors.New("queue symlink forbidden")
	}
	lock, e := os.OpenFile(filepath.Join(q.Dir, "delivery.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return nil, errors.New("delivery already active")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	id := r.Identity
	id.Revision = ""
	key, _ := proof.Canonical(id)
	path := filepath.Join(q.Dir, proof.Digest(key)+".json")
	s := &State{Result: *r, Status: "pending", Failures: []api.Attempt{}}
	old, e := proof.ReadBounded(path, 64<<10)
	if e == nil {
		if json.Unmarshal(old, s) != nil {
			return nil, errors.New("invalid queue state")
		}
		expected, _ := proof.Canonical(s)
		if !bytes.Equal(old, expected) || s.Result != *r || s.Tries < 0 || s.Tries > 6 || len(s.Failures) > 6 {
			return nil, errors.New("queue identity/state conflict")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	// An interrupted request consumed an attempt. Preserve that fact even when
	// the process died before writing its failure report; reconciliation below
	// can still recognize a commit whose response was lost.
	if s.Status == "running" {
		s.Status = "retry"
		if s.Tries >= 6 {
			s.Status = "exhausted"
			s.NextRetryAt = nil
		}
		s.Failures = append(s.Failures, api.Attempt{Identity: r.Identity, Number: s.Tries, Stage: "ingest", Result: s.Status, ErrorCode: "dependency_unavailable", NextRetryAt: s.NextRetryAt})
	}
	if e = save(path, s); e != nil {
		return nil, e
	}
	flushed := q.flush(ctx, s)
	if s.Status == "complete" {
		if !flushed {
			return s, errors.New("attempt reports awaiting recovery")
		}
		return s, nil
	}
	// Reconcile ambiguous commits even after the sixth try or before retry due.
	if q.reconcile(ctx, s) {
		return s, save(path, s)
	}
	if s.Status == "exhausted" || s.Tries >= 6 {
		return s, errors.New("delivery exhausted; archives retained")
	}
	if s.NextRetryAt != nil && q.now().Before(*s.NextRetryAt) {
		return s, errors.New("delivery retry not due")
	}
	// Persist an in-flight attempt before network I/O. A crash cannot reset budget.
	s.Tries++
	s.Status = "running"
	due := q.now().Add(delay(s.Tries))
	s.NextRetryAt = &due
	if e = save(path, s); e != nil {
		return nil, e
	}
	status, b, requestErr := q.request(ctx, "POST", "/v1/receipts", data)
	var result api.Commit
	if requestErr == nil && (status == 200 || status == 201) && json.Unmarshal(b, &result) == nil && result.ID != "" {
		s.Status = "complete"
		s.ReceiptID = result.ID
		s.NextRetryAt = nil
		return s, save(path, s)
	}
	code := "dependency_unavailable"
	s.Status = "retry"
	if requestErr == nil && (status == 409 || status == 422 || status == 400 || status == 401 || status == 403 || status == 415 || status == 413) {
		s.Status = "exhausted"
		code = "invalid_evidence"
		if status == 409 {
			code = "identity_conflict"
		}
	}
	if s.Tries == 6 {
		s.Status = "exhausted"
	}
	if s.Status == "exhausted" {
		s.NextRetryAt = nil
	}
	s.Failures = append(s.Failures, api.Attempt{Identity: r.Identity, Number: s.Tries, Stage: "ingest", Result: s.Status, ErrorCode: code, NextRetryAt: s.NextRetryAt})
	if e = save(path, s); e != nil {
		return nil, e
	}
	q.flush(ctx, s)
	return s, errors.New("delivery pending or exhausted; signed evidence retained")
}
func delay(attempt int) time.Duration {
	return []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour, time.Hour}[attempt-1]
}
