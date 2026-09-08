package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }
type Commit struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
}

func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }

func (s *Store) Ingest(ctx context.Context, v *Verified) (Commit, error) {
	var out Commit
	r := v.Receipt
	id := r.Identity
	if !validIdentity(id) {
		return out, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	err = tx.QueryRow(ctx, `INSERT INTO evidence.receipts
 (provider,repository_id,repository,workflow,run_id,run_attempt,job_id,job_name,source_revision,verdict,started_at,completed_at,finalized_at,signature_state,signer_issuer,signer_identity,trust_root_sha256,receipt_object,bundle_object,log_objects,coverage,resource_summary)
 VALUES ($1,$2,$2,CASE WHEN $1='github' THEN 'GitHub Actions' ELSE 'Local CI' END,$3,$4,$5,$5,$6,$7,$8,$9,$10,'verified',$11,$12,$13,$14,$15,$16,$17,$18)
 ON CONFLICT ON CONSTRAINT receipts_run_identity DO NOTHING RETURNING id::text`, id.Provider, id.Repository, id.Run, id.Attempt, id.Job, id.Revision, r.Verdict, r.StartedAt, r.CompletedAt, r.FinalizedAt, r.Finalizer.Issuer, r.Finalizer.Identity, r.Finalizer.TrustRootSHA256, jsonBytes(v.Result.Receipt), jsonBytes(v.Result.Bundle), jsonBytes(r.LogObjects), jsonBytes(sanitizedCoverage(r)), jsonBytes(map[string]any{"evidence_status": r.EvidenceStatus, "finalizer_revision": r.Finalizer.Revision, "resource_namespace": r.Binding.ResourceNamespace, "runner_namespace": r.Binding.RunnerNamespace})).Scan(&out.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A second statement obtains a fresh READ COMMITTED snapshot after waiting
		// for a concurrent inserter. Compare both immutable references, not a claim.
		var same bool
		err = tx.QueryRow(ctx, `SELECT id::text, receipt_object=$6::jsonb AND bundle_object=$7::jsonb AND source_revision=$8 FROM evidence.receipts WHERE provider=$1 AND repository_id=$2 AND run_id=$3 AND run_attempt=$4 AND job_id=$5`, id.Provider, id.Repository, id.Run, id.Attempt, id.Job, jsonBytes(v.Result.Receipt), jsonBytes(v.Result.Bundle), id.Revision).Scan(&out.ID, &same)
		if err != nil {
			return out, ErrUnavailable
		}
		if !same {
			return out, ErrConflict
		}
		out.Duplicate = true
	} else if err != nil {
		return out, ErrUnavailable
	}
	if !out.Duplicate && r.Verdict != "pass" {
		_, err = tx.Exec(ctx, `INSERT INTO evidence.incidents(receipt_id,issue_key,reason_code,reason) VALUES($1,$2,$3,$4)`, out.ID, "cleanup:"+out.ID, "cleanup_"+r.Verdict, "Cleanup "+r.Verdict+". Inspect component coverage and independently verify the archived evidence.")
		if err != nil {
			return out, ErrUnavailable
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Commit{}, ErrUnavailable
	}
	return out, nil
}

const projection = `(to_jsonb(r)-'search_document') || jsonb_build_object('incident',to_jsonb(i))`
const joined = ` FROM evidence.receipts r LEFT JOIN evidence.incidents i ON i.receipt_id=r.id `

func (s *Store) Detail(ctx context.Context, id string) (json.RawMessage, error) {
	if !uuidPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	var b []byte
	err := s.Pool.QueryRow(ctx, `SELECT `+projection+joined+` WHERE r.id=$1`, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	return b, nil
}
func (s *Store) Result(ctx context.Context, id string) (*finalizer.Result, error) {
	b, e := s.Detail(ctx, id)
	if e != nil {
		return nil, e
	}
	var row struct {
		Provider   string          `json:"provider"`
		Repository string          `json:"repository"`
		Run        string          `json:"run_id"`
		Attempt    int             `json:"run_attempt"`
		Job        string          `json:"job_id"`
		Revision   string          `json:"source_revision"`
		Receipt    json.RawMessage `json:"receipt_object"`
		Bundle     json.RawMessage `json:"bundle_object"`
	}
	if json.Unmarshal(b, &row) != nil {
		return nil, ErrUnavailable
	}
	r := &finalizer.Result{Kind: "cleanup-finalization-result/v1", SignatureState: "verified", Identity: proof.Identity{Provider: row.Provider, Repository: row.Repository, Run: row.Run, Attempt: row.Attempt, Job: row.Job, Revision: row.Revision}}
	if json.Unmarshal(row.Receipt, &r.Receipt) != nil || json.Unmarshal(row.Bundle, &r.Bundle) != nil {
		return nil, ErrUnavailable
	}
	return r, nil
}

type Page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type Filter struct {
	Repository, Verdict, Incident, Search, Run string
	From, To, Before                           time.Time
	BeforeID                                   string
	Limit                                      int
}

func ParseFilter(q url.Values) (Filter, error) {
	f := Filter{Limit: 25}
	for k, v := range q {
		if len(v) != 1 || len(v[0]) > 512 || !strings.Contains("|repository|verdict|incident_state|q|run_id|from|to|cursor|limit|", "|"+k+"|") {
			return f, ErrInvalid
		}
	}
	f.Repository = q.Get("repository")
	f.Verdict = q.Get("verdict")
	f.Incident = q.Get("incident_state")
	f.Search = q.Get("q")
	f.Run = q.Get("run_id")
	if len(f.Search) > 255 || (f.Verdict != "" && f.Verdict != "pass" && f.Verdict != "fail" && f.Verdict != "partial") || (f.Incident != "" && f.Incident != "open" && f.Incident != "resolved" && f.Incident != "none") {
		return f, ErrInvalid
	}
	var e error
	if q.Has("limit") {
		f.Limit, e = strconv.Atoi(q.Get("limit"))
		if e != nil || f.Limit < 1 || f.Limit > 100 {
			return f, ErrInvalid
		}
	}
	for k, target := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if q.Has(k) {
			*target, e = time.Parse(time.RFC3339Nano, q.Get(k))
			if e != nil {
				return f, ErrInvalid
			}
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.To.After(f.From) {
		return f, ErrInvalid
	}
	if q.Has("cursor") {
		b, e := base64.RawURLEncoding.DecodeString(q.Get("cursor"))
		var c []string
		if e != nil || json.Unmarshal(b, &c) != nil || len(c) != 2 || !uuidPattern.MatchString(c[1]) {
			return f, ErrInvalid
		}
		f.Before, e = time.Parse(time.RFC3339Nano, c[0])
		if e != nil {
			return f, ErrInvalid
		}
		f.BeforeID = c[1]
	}
	return f, nil
}
func (s *Store) List(ctx context.Context, f Filter, incidents bool) (Page, error) {
	out := Page{Items: []json.RawMessage{}}
	where := []string{"true"}
	args := []any{}
	add := func(expr string, v any) { args = append(args, v); where = append(where, fmt.Sprintf(expr, len(args))) }
	if incidents {
		where = append(where, "i.id IS NOT NULL")
	}
	if f.Repository != "" {
		add("r.repository=$%d", f.Repository)
	}
	if f.Run != "" {
		add("r.run_id=$%d", f.Run)
	}
	if f.Verdict != "" {
		add("r.verdict=$%d", f.Verdict)
	}
	if f.Incident == "none" {
		where = append(where, "i.id IS NULL")
	} else if f.Incident != "" {
		add("i.state=$%d", f.Incident)
	}
	if f.Search != "" {
		add("r.search_document @@ plainto_tsquery('simple',$%d)", f.Search)
	}
	if !f.From.IsZero() {
		add("r.completed_at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("r.completed_at < $%d", f.To)
	}
	if !f.Before.IsZero() {
		args = append(args, f.Before, f.BeforeID)
		where = append(where, fmt.Sprintf("(r.completed_at,r.id)<($%d,$%d::uuid)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)
	rows, err := s.Pool.Query(ctx, `SELECT `+projection+`,r.completed_at,r.id::text`+joined+` WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY r.completed_at DESC,r.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	var lastTime time.Time
	var lastID string
	for rows.Next() {
		var b []byte
		var when time.Time
		var id string
		if rows.Scan(&b, &when, &id) != nil {
			return out, ErrUnavailable
		}
		if len(out.Items) == f.Limit {
			out.NextCursor = base64.RawURLEncoding.EncodeToString(jsonBytes([]string{lastTime.Format(time.RFC3339Nano), lastID}))
			break
		}
		out.Items = append(out.Items, json.RawMessage(b))
		lastTime = when
		lastID = id
	}
	if rows.Err() != nil {
		return out, ErrUnavailable
	}
	return out, nil
}

// Attempts here are immutable reports of completed operational failures.
// The watchdog schedules recovery separately. Success is proven by a receipt query.
type Attempt struct {
	Identity    proof.Identity `json:"identity"`
	Number      int            `json:"attempt_number"`
	Stage       string         `json:"stage"`
	Result      string         `json:"result"`
	ErrorCode   string         `json:"error_code"`
	NextRetryAt *time.Time     `json:"next_retry_at"`
}

func (a Attempt) Validate() bool {
	if !validIdentity(a.Identity) || a.Number < 1 || a.Number > 6 {
		return false
	}
	if a.Stage != "collect" && a.Stage != "archive" && a.Stage != "sign" && a.Stage != "ingest" {
		return false
	}
	if a.ErrorCode != "dependency_unavailable" && a.ErrorCode != "invalid_evidence" && a.ErrorCode != "identity_conflict" {
		return false
	}
	return (a.Result == "retry" && a.Number < 6 && a.NextRetryAt != nil) || (a.Result == "exhausted" && a.NextRetryAt == nil)
}
func (s *Store) RecordAttempt(ctx context.Context, a Attempt) (Commit, error) {
	out := Commit{}
	if !a.Validate() {
		return out, ErrInvalid
	}
	// Report time is server-owned. A recovered overdue retry remains immediately
	// actionable while satisfying the schema's strictly-later retry timestamp.
	id := a.Identity
	err := s.Pool.QueryRow(ctx, `INSERT INTO evidence.finalization_attempts(provider,repository_id,run_id,run_attempt,job_id,attempt_number,stage,result,error_code,started_at,finished_at,next_retry_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now(),CASE WHEN $8='retry' THEN greatest($10::timestamptz,now()+interval '1 microsecond') ELSE NULL END)
 ON CONFLICT ON CONSTRAINT finalization_attempt_identity DO NOTHING RETURNING id::text`, id.Provider, id.Repository, id.Run, id.Attempt, id.Job, a.Number, a.Stage, a.Result, a.ErrorCode, a.NextRetryAt).Scan(&out.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var same bool
		err = s.Pool.QueryRow(ctx, `SELECT id::text,stage=$7 AND result=$8 AND error_code=$9 FROM evidence.finalization_attempts WHERE provider=$1 AND repository_id=$2 AND run_id=$3 AND run_attempt=$4 AND job_id=$5 AND attempt_number=$6 AND origin='delivery'`, id.Provider, id.Repository, id.Run, id.Attempt, id.Job, a.Number, a.Stage, a.Result, a.ErrorCode).Scan(&out.ID, &same)
		if err == nil && !same {
			return out, ErrConflict
		}
		out.Duplicate = true
	}
	if err != nil {
		return out, ErrUnavailable
	}
	return out, nil
}
func (s *Store) Attempts(ctx context.Context, run string) ([]json.RawMessage, error) {
	if run != "" && !regexpRun.MatchString(run) {
		return nil, ErrInvalid
	}
	rows, e := s.Pool.Query(ctx, `SELECT to_jsonb(a) FROM evidence.finalization_attempts a WHERE ($1='' OR run_id=$1) ORDER BY created_at DESC,id DESC LIMIT 100`, run)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if rows.Scan(&b) != nil {
			return nil, ErrUnavailable
		}
		out = append(out, b)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}
