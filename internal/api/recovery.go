package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
	"github.com/jackc/pgx/v5"
	"time"
)

func validRecovery(a watchdog.Report) bool {
	if !validIdentity(a.Identity) || a.Number < 1 || a.Number > 6 {
		return false
	}
	switch a.Status {
	case "running":
		return (a.Stage == "collect" || a.Stage == "archive" || a.Stage == "sign" || a.Stage == "ingest") && uuidPattern.MatchString(a.LeaseOwner) && a.LeaseExpiresAt != nil && a.NextRetryAt == nil && a.ReceiptID == "" && a.ErrorCode == ""
	case "succeeded":
		return a.Stage == "complete" && uuidPattern.MatchString(a.ReceiptID) && a.NextRetryAt == nil && a.LeaseOwner == "" && a.LeaseExpiresAt == nil && a.ErrorCode == ""
	case "retry", "exhausted":
		return (a.Stage == "collect" || a.Stage == "archive" || a.Stage == "sign" || a.Stage == "ingest") && (a.ErrorCode == "dependency_unavailable" || a.ErrorCode == "invalid_evidence" || a.ErrorCode == "worker_interrupted" || a.ErrorCode == "stage_exhausted") && a.LeaseOwner == "" && a.LeaseExpiresAt == nil && a.ReceiptID == "" && ((a.Status == "retry" && a.Number < 6 && a.NextRetryAt != nil) || (a.Status == "exhausted" && a.NextRetryAt == nil))
	}
	return false
}
func (s *Store) Recovery(ctx context.Context, a watchdog.Report) error {
	if !validRecovery(a) {
		return ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	id := a.Identity
	// An operational reporter cannot invent successful completion. Link only a
	// receipt the ingestion API already independently verified for this identity.
	if a.Status == "succeeded" {
		var ok bool
		e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM evidence.receipts WHERE id=$1 AND provider=$2 AND repository_id=$3 AND run_id=$4 AND run_attempt=$5 AND job_id=$6)", a.ReceiptID, id.Provider, id.Repository, id.Run, id.Attempt, id.Job).Scan(&ok)
		if e != nil {
			return ErrUnavailable
		}
		if !ok {
			return ErrInvalid
		}
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT now()").Scan(&now); e != nil {
		return ErrUnavailable
	}
	var lease, next *time.Time
	if a.LeaseExpiresAt != nil {
		v := *a.LeaseExpiresAt
		if !v.After(now) {
			v = now.Add(time.Microsecond)
		}
		if v.After(now.Add(15 * time.Minute)) {
			return ErrInvalid
		}
		lease = &v
	}
	if a.NextRetryAt != nil {
		v := *a.NextRetryAt
		if !v.After(now) {
			v = now.Add(time.Microsecond)
		}
		if v.After(now.Add(2 * time.Hour)) {
			return ErrInvalid
		}
		next = &v
	}
	_, e = tx.Exec(ctx, `INSERT INTO evidence.finalization_attempts(provider,repository_id,run_id,run_attempt,job_id,attempt_number,origin,stage,result,error_code,receipt_id,started_at,finished_at,next_retry_at,lease_owner,lease_expires_at)
 VALUES($1,$2,$3,$4,$5,$6,'watchdog',$7,$8,nullif($9,''),nullif($10,'')::uuid,now(),CASE WHEN $8='running' THEN NULL ELSE now() END,$11,nullif($12,'')::uuid,$13)
 ON CONFLICT ON CONSTRAINT finalization_attempt_identity DO UPDATE SET stage=EXCLUDED.stage,result=EXCLUDED.result,error_code=EXCLUDED.error_code,receipt_id=EXCLUDED.receipt_id,finished_at=EXCLUDED.finished_at,next_retry_at=EXCLUDED.next_retry_at,lease_owner=EXCLUDED.lease_owner,lease_expires_at=EXCLUDED.lease_expires_at
 WHERE finalization_attempts.result='running' OR (EXCLUDED.result='succeeded' AND finalization_attempts.result<>'succeeded')`,
		id.Provider, id.Repository, id.Run, id.Attempt, id.Job, a.Number, a.Stage, a.Status, a.ErrorCode, a.ReceiptID, next, a.LeaseOwner, lease)
	if e != nil {
		return ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) Recoveries(ctx context.Context) ([]json.RawMessage, error) {
	rows, e := s.Pool.Query(ctx, `SELECT to_jsonb(v) FROM (SELECT DISTINCT ON(provider,repository_id,run_id,run_attempt,job_id) id,provider,repository_id,run_id,run_attempt,job_id,attempt_number,stage,result,error_code,receipt_id,created_at,started_at,finished_at,next_retry_at,lease_expires_at FROM evidence.finalization_attempts WHERE origin='watchdog' ORDER BY provider,repository_id,run_id,run_attempt,job_id,attempt_number DESC) v ORDER BY (result='succeeded'),coalesce(finished_at,started_at) DESC LIMIT 100`)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, ErrUnavailable
		}
		out = append(out, b)
	}
	if e = rows.Err(); e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrUnavailable
	}
	return out, nil
}
