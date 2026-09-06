// Package dbfixture contains synthetic metadata for isolated schema checks only.
// It does not sign receipts or verify signatures and must not be used by ingestion.
package dbfixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

type Execer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Coverage(verdict string) string {
	components := map[string]map[string]string{}
	for _, component := range []string{"workspace", "credentials", "logs", "resources", "runner_disposal"} {
		components[component] = map[string]string{"status": "verified", "observer": "trusted", "reason": "SYNTHETIC SCHEMA TEST ONLY"}
	}
	if verdict == "fail" {
		components["resources"]["status"] = "failed"
	}
	if verdict == "partial" {
		components["runner_disposal"]["status"] = "unobservable"
	}
	data, _ := json.Marshal(components)
	return string(data)
}

func Object() string {
	return `{"store":"schema-test-only","bucket":"test","key":"not-real-evidence","version_id":"fixture","sha256":"` + strings.Repeat("a", 64) + `","size_bytes":1}`
}

const InsertSQL = `INSERT INTO evidence.receipts
 (provider,repository_id,repository,workflow,run_id,run_attempt,job_id,job_name,source_revision,verdict,
 started_at,completed_at,finalized_at,signature_state,signer_issuer,signer_identity,trust_root_sha256,
 receipt_object,bundle_object,log_objects,coverage)
 VALUES('local','schema-test-repo','schema-test/repository','schema-test-workflow',$1,1,'job-1','schema-test-job',
 repeat('a',40),$2,now(),now(),now(),'verified','https://schema-test.invalid','schema-test-only',repeat('b',64),
 $3::jsonb,$3::jsonb,jsonb_build_array($3::jsonb),$4::jsonb) RETURNING id::text`

func Insert(ctx context.Context, tx Execer, run, verdict string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, InsertSQL, run, verdict, Object(), Coverage(verdict)).Scan(&id)
	return id, err
}

const IncidentSQL = `INSERT INTO evidence.incidents(receipt_id,issue_key,reason_code,reason)
 VALUES($1::uuid,$1::text,'schema_test_only','SYNTHETIC METADATA: no cleanup or signature verification occurred')`
