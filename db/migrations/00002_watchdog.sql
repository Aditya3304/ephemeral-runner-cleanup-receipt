-- +goose Up
ALTER TABLE evidence.finalization_attempts ADD COLUMN origin text NOT NULL DEFAULT 'delivery' CHECK (origin IN ('delivery','watchdog'));
ALTER TABLE evidence.finalization_attempts DROP CONSTRAINT finalization_attempt_identity;
ALTER TABLE evidence.finalization_attempts ADD CONSTRAINT finalization_attempt_identity UNIQUE(provider,repository_id,run_id,run_attempt,job_id,attempt_number,origin);
CREATE INDEX recovery_latest ON evidence.finalization_attempts(provider,repository_id,run_id,run_attempt,job_id,attempt_number DESC) WHERE origin='watchdog';
GRANT UPDATE(error_code,receipt_id,stage,result,started_at,finished_at,next_retry_at,lease_owner,lease_expires_at) ON evidence.finalization_attempts TO proof_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM evidence.finalization_attempts WHERE origin='watchdog') THEN RAISE EXCEPTION 'Preserve watchdog history before downgrading'; END IF; END $$;
-- +goose StatementEnd
DROP INDEX evidence.recovery_latest;
ALTER TABLE evidence.finalization_attempts DROP CONSTRAINT finalization_attempt_identity;
ALTER TABLE evidence.finalization_attempts DROP COLUMN origin;
ALTER TABLE evidence.finalization_attempts ADD CONSTRAINT finalization_attempt_identity UNIQUE(provider,repository_id,run_id,run_attempt,job_id,attempt_number);
