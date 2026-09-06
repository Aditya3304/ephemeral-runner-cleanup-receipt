-- +goose Up
CREATE SCHEMA evidence;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA evidence FROM PUBLIC;

-- A bounded reference to an immutable object version, not an arbitrary fetch URL.
-- +goose StatementBegin
CREATE FUNCTION evidence.valid_object_ref(value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
  SELECT COALESCE(
    jsonb_typeof(value) = 'object'
    AND value ?& ARRAY['store','bucket','key','version_id','sha256','size_bytes']
    AND (value - ARRAY['store','bucket','key','version_id','sha256','size_bytes']) = '{}'::jsonb
    AND jsonb_typeof(value->'store') = 'string' AND length(value->>'store') BETWEEN 1 AND 80
    AND jsonb_typeof(value->'bucket') = 'string' AND length(value->>'bucket') BETWEEN 1 AND 63
    AND jsonb_typeof(value->'key') = 'string' AND length(value->>'key') BETWEEN 1 AND 1024
    AND jsonb_typeof(value->'version_id') = 'string' AND length(value->>'version_id') BETWEEN 1 AND 1024
    AND jsonb_typeof(value->'sha256') = 'string' AND (value->>'sha256') ~ '^[0-9a-f]{64}$'
    AND jsonb_typeof(value->'size_bytes') = 'number' AND (value->>'size_bytes') ~ '^[0-9]{1,12}$'
    AND pg_column_size(value) <= 4096, false);
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION evidence.valid_log_refs(value jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE item jsonb;
BEGIN
  IF jsonb_typeof(value) <> 'array' THEN RETURN false; END IF;
  IF jsonb_array_length(value) > 64 THEN RETURN false; END IF;
  FOR item IN SELECT * FROM jsonb_array_elements(value) LOOP
    IF NOT evidence.valid_object_ref(item) THEN RETURN false; END IF;
  END LOOP;
  RETURN true;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION evidence.valid_coverage(value jsonb, verdict text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE component text; item jsonb; has_failure boolean := false; all_verified boolean := true;
BEGIN
  IF jsonb_typeof(value) <> 'object' OR pg_column_size(value) > 16384 THEN RETURN false; END IF;
  IF NOT value ?& ARRAY['workspace','credentials','logs','resources','runner_disposal']
     OR (value - ARRAY['workspace','credentials','logs','resources','runner_disposal']) <> '{}'::jsonb
     THEN RETURN false; END IF;
  FOREACH component IN ARRAY ARRAY['workspace','credentials','logs','resources','runner_disposal'] LOOP
    item := value->component;
    IF jsonb_typeof(item) <> 'object'
       OR NOT item ?& ARRAY['status','observer','reason']
       OR (item - ARRAY['status','observer','reason']) <> '{}'::jsonb
       OR jsonb_typeof(item->'status') IS DISTINCT FROM 'string'
       OR jsonb_typeof(item->'observer') IS DISTINCT FROM 'string'
       OR jsonb_typeof(item->'reason') IS DISTINCT FROM 'string'
       OR length(item->>'reason') NOT BETWEEN 1 AND 512
       OR item->>'status' NOT IN ('verified','failed','unsupported','unobservable')
       OR item->>'observer' NOT IN ('trusted','job','none') THEN RETURN false; END IF;
    IF item->>'status' = 'failed' THEN has_failure := true; END IF;
    IF item->>'status' <> 'verified' OR item->>'observer' <> 'trusted' THEN all_verified := false; END IF;
  END LOOP;
  RETURN CASE verdict
    WHEN 'pass' THEN all_verified
    WHEN 'fail' THEN has_failure
    WHEN 'partial' THEN NOT has_failure AND NOT all_verified
    ELSE false END;
END;
$$;
-- +goose StatementEnd

CREATE TABLE evidence.receipts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version = 1),
  provider text NOT NULL CHECK (provider IN ('local','github')),
  repository_id text NOT NULL CHECK (length(repository_id) BETWEEN 1 AND 255),
  repository text NOT NULL CHECK (length(repository) BETWEEN 1 AND 255),
  workflow text NOT NULL CHECK (length(workflow) BETWEEN 1 AND 255),
  run_id text NOT NULL CHECK (length(run_id) BETWEEN 1 AND 255),
  run_attempt integer NOT NULL CHECK (run_attempt > 0),
  job_id text NOT NULL CHECK (length(job_id) BETWEEN 1 AND 255),
  job_name text NOT NULL CHECK (length(job_name) BETWEEN 1 AND 255),
  source_revision text NOT NULL CHECK (source_revision ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  verdict text NOT NULL CHECK (verdict IN ('pass','fail','partial')),
  started_at timestamptz NOT NULL,
  completed_at timestamptz NOT NULL CHECK (completed_at >= started_at),
  finalized_at timestamptz NOT NULL CHECK (finalized_at >= completed_at),
  ingested_at timestamptz NOT NULL DEFAULT now(),
  signature_state text NOT NULL CHECK (signature_state = 'verified'),
  signer_issuer text NOT NULL CHECK (length(signer_issuer) BETWEEN 1 AND 1024),
  signer_identity text NOT NULL CHECK (length(signer_identity) BETWEEN 1 AND 1024),
  trust_root_sha256 text NOT NULL CHECK (trust_root_sha256 ~ '^[0-9a-f]{64}$'),
  receipt_object jsonb NOT NULL CHECK (evidence.valid_object_ref(receipt_object)),
  bundle_object jsonb NOT NULL CHECK (evidence.valid_object_ref(bundle_object)),
  log_objects jsonb NOT NULL CHECK (evidence.valid_log_refs(log_objects)),
  coverage jsonb NOT NULL CHECK (evidence.valid_coverage(coverage, verdict)),
  resource_summary jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(resource_summary) = 'object' AND pg_column_size(resource_summary) <= 16384),
  search_document tsvector GENERATED ALWAYS AS (
    to_tsvector('simple', repository || ' ' || workflow || ' ' || job_name || ' ' || run_id || ' ' || job_id)
  ) STORED,
  CONSTRAINT receipts_run_identity UNIQUE(provider, repository_id, run_id, run_attempt, job_id),
  CONSTRAINT receipts_link_identity UNIQUE(id, provider, repository_id, run_id, run_attempt, job_id),
  CONSTRAINT pass_requires_logs CHECK (verdict <> 'pass' OR jsonb_array_length(log_objects) > 0)
);

CREATE TABLE evidence.incidents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  receipt_id uuid NOT NULL UNIQUE REFERENCES evidence.receipts(id) ON DELETE RESTRICT,
  issue_key text NOT NULL UNIQUE CHECK (length(issue_key) BETWEEN 1 AND 512),
  state text NOT NULL DEFAULT 'open' CHECK (state IN ('open','resolved')),
  reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_]{0,79}$'),
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
  github_issue_url text CHECK (github_issue_url ~ '^https://github[.]com/[^/]+/[^/]+/issues/[0-9]+$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  CHECK ((state = 'open' AND resolved_at IS NULL) OR (state = 'resolved' AND resolved_at IS NOT NULL AND resolved_at >= created_at))
);

-- Deferred so the receipt and incident may be inserted in either order in one transaction.
-- +goose StatementBegin
CREATE FUNCTION evidence.enforce_incident_pair() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; target_verdict text; incident_count integer;
BEGIN
  IF TG_TABLE_NAME = 'receipts' THEN target := COALESCE(NEW.id, OLD.id);
  ELSE target := COALESCE(NEW.receipt_id, OLD.receipt_id); END IF;
  SELECT verdict INTO target_verdict FROM evidence.receipts WHERE id = target;
  IF NOT FOUND THEN RETURN NULL; END IF;
  SELECT count(*) INTO incident_count FROM evidence.incidents WHERE receipt_id = target;
  IF (target_verdict = 'pass' AND incident_count <> 0)
     OR (target_verdict <> 'pass' AND incident_count <> 1) THEN
    RAISE EXCEPTION 'receipt verdict and incident must be committed together'
      USING ERRCODE = '23514', CONSTRAINT = 'receipt_incident_pair';
  END IF;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER receipt_incident_pair AFTER INSERT OR UPDATE ON evidence.receipts
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION evidence.enforce_incident_pair();
CREATE CONSTRAINT TRIGGER incident_receipt_pair AFTER INSERT OR UPDATE OR DELETE ON evidence.incidents
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION evidence.enforce_incident_pair();

CREATE TABLE evidence.finalization_attempts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider text NOT NULL CHECK (provider IN ('local','github')),
  repository_id text NOT NULL CHECK (length(repository_id) BETWEEN 1 AND 255),
  run_id text NOT NULL CHECK (length(run_id) BETWEEN 1 AND 255),
  run_attempt integer NOT NULL CHECK (run_attempt > 0),
  job_id text NOT NULL CHECK (length(job_id) BETWEEN 1 AND 255),
  attempt_number integer NOT NULL CHECK (attempt_number BETWEEN 1 AND 6),
  stage text NOT NULL CHECK (stage IN ('pending','collect','archive','sign','ingest','complete')),
  result text NOT NULL CHECK (result IN ('pending','running','succeeded','retry','exhausted')),
  error_code text CHECK (error_code ~ '^[a-z][a-z0-9_]{0,79}$'),
  receipt_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz,
  next_retry_at timestamptz,
  lease_owner uuid,
  lease_expires_at timestamptz,
  CONSTRAINT finalization_attempt_identity UNIQUE(provider, repository_id, run_id, run_attempt, job_id, attempt_number),
  FOREIGN KEY(receipt_id, provider, repository_id, run_id, run_attempt, job_id)
    REFERENCES evidence.receipts(id, provider, repository_id, run_id, run_attempt, job_id) ON DELETE RESTRICT,
  CHECK (started_at IS NULL OR started_at >= created_at),
  CHECK (finished_at IS NULL OR (started_at IS NOT NULL AND finished_at >= started_at)),
  CHECK ((result = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL AND lease_expires_at > started_at AND started_at IS NOT NULL)
    OR (result <> 'running' AND lease_owner IS NULL AND lease_expires_at IS NULL)),
  CHECK (CASE result
    WHEN 'pending' THEN stage = 'pending' AND started_at IS NULL AND finished_at IS NULL AND error_code IS NULL AND next_retry_at IS NULL AND receipt_id IS NULL
    WHEN 'running' THEN stage IN ('collect','archive','sign','ingest') AND finished_at IS NULL AND error_code IS NULL AND next_retry_at IS NULL AND receipt_id IS NULL
    WHEN 'succeeded' THEN stage = 'complete' AND finished_at IS NOT NULL AND receipt_id IS NOT NULL AND error_code IS NULL AND next_retry_at IS NULL
    WHEN 'retry' THEN stage IN ('collect','archive','sign','ingest') AND finished_at IS NOT NULL AND error_code IS NOT NULL AND next_retry_at IS NOT NULL AND next_retry_at > finished_at AND receipt_id IS NULL AND attempt_number < 6
    WHEN 'exhausted' THEN stage IN ('collect','archive','sign','ingest') AND finished_at IS NOT NULL AND error_code IS NOT NULL AND next_retry_at IS NULL AND receipt_id IS NULL
    ELSE false END)
);

CREATE INDEX receipts_repository_completed ON evidence.receipts(repository, completed_at DESC, id);
CREATE INDEX receipts_verdict_completed ON evidence.receipts(verdict, completed_at DESC, id);
CREATE INDEX receipts_completed ON evidence.receipts(completed_at DESC, id);
CREATE INDEX receipts_search ON evidence.receipts USING gin(search_document);
CREATE INDEX incidents_open_created ON evidence.incidents(created_at DESC, id) WHERE state = 'open';
CREATE INDEX finalization_retry_due ON evidence.finalization_attempts(next_retry_at, id) WHERE result = 'retry';
CREATE INDEX finalization_lease_due ON evidence.finalization_attempts(lease_expires_at, id) WHERE result = 'running';

GRANT USAGE ON SCHEMA evidence TO proof_api;
GRANT SELECT, INSERT ON evidence.receipts, evidence.incidents, evidence.finalization_attempts TO proof_api;
GRANT UPDATE(state, resolved_at, github_issue_url) ON evidence.incidents TO proof_api;
GRANT UPDATE(stage, result, error_code, receipt_id, started_at, finished_at, next_retry_at, lease_owner, lease_expires_at)
  ON evidence.finalization_attempts TO proof_api;

-- +goose Down
DROP TABLE evidence.finalization_attempts;
DROP TABLE evidence.incidents;
DROP TABLE evidence.receipts;
DROP FUNCTION evidence.enforce_incident_pair();
DROP FUNCTION evidence.valid_coverage(jsonb, text);
DROP FUNCTION evidence.valid_log_refs(jsonb);
DROP FUNCTION evidence.valid_object_ref(jsonb);
DROP SCHEMA evidence;
-- Intentionally do not restore PUBLIC schema-write privileges on downgrade.
