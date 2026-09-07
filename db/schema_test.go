package db_test

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/db"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/database"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/dbfixture"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, err)
	}
}

func exec(t *testing.T, conn *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

// These tests use an isolated, randomly named PostgreSQL database. Fixtures are
// deliberately synthetic; none is evidence of signature or cleanup verification.
func TestSchema(t *testing.T) {
	ctx := context.Background()
	root, err := database.Open(ctx, "proof", true)
	if err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "proofcheck_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	exec(t, root, "CREATE DATABASE "+quoted)
	var admin, api *sql.DB
	t.Cleanup(func() {
		if api != nil {
			api.Close()
		}
		if admin != nil {
			admin.Close()
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// This name was generated and created above; no other database is eligible.
		if _, err := root.ExecContext(cleanupCtx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("temporary database cleanup: %v", err)
		}
		root.Close()
	})
	admin, err = database.Open(ctx, name, true)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, root, "REVOKE ALL ON DATABASE "+quoted+" FROM PUBLIC")
	exec(t, root, "GRANT CONNECT ON DATABASE "+quoted+" TO proof_api")

	t.Run("fresh_migration_idempotence_down_and_up", func(t *testing.T) {
		for _, command := range []string{"up", "up", "reset", "up"} {
			if err := db.Migrate(ctx, admin, command); err != nil {
				t.Fatal(err)
			}
			if command == "reset" {
				var count int
				if err := admin.QueryRow("SELECT count(*) FROM information_schema.schemata WHERE schema_name='evidence'").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("downgrade left evidence schema")
				}
			}
		}
		var tables int
		if err := admin.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_schema='evidence'").Scan(&tables); err != nil {
			t.Fatal(err)
		}
		if tables != 3 {
			t.Fatalf("expected three application tables, got %d", tables)
		}
	})
	api, err = database.Open(ctx, name, false)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("tls_verified_and_plaintext_bad_ca_bad_hostname_bad_password_rejected", func(t *testing.T) {
		var ssl bool
		if err := admin.QueryRow("SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&ssl); err != nil || !ssl {
			t.Fatalf("TLS required: %v", err)
		}
		for _, kind := range []string{"plaintext", "wrong-ca", "wrong-hostname", "wrong-password"} {
			t.Run(kind, func(t *testing.T) {
				cfg, err := database.Config(name, false)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Fallbacks = nil
				switch kind {
				case "plaintext":
					cfg.TLSConfig = nil
				case "wrong-ca":
					cfg.TLSConfig.RootCAs = x509.NewCertPool()
				case "wrong-hostname":
					cfg.TLSConfig.ServerName = "not-the-database.invalid"
				case "wrong-password":
					cfg.Password = "deliberately-incorrect"
				}
				conn, err := pgx.ConnectConfig(ctx, cfg)
				if err == nil {
					conn.Close(ctx)
					t.Fatal("invalid connection unexpectedly accepted")
				}
			})
		}
	})

	t.Run("api_role_cannot_administer_modify_receipts_or_touch_migrations", func(t *testing.T) {
		for _, query := range []string{
			"CREATE TABLE evidence.forbidden(id int)",
			"CREATE TABLE public.forbidden(id int)",
			"CREATE ROLE forbidden_user",
			"UPDATE evidence.receipts SET verdict='pass'",
			"DELETE FROM evidence.receipts",
			"DELETE FROM evidence.incidents",
			"UPDATE evidence.incidents SET receipt_id=gen_random_uuid()",
			"UPDATE public.goose_db_version SET is_applied=false",
		} {
			_, err := api.Exec(query)
			requireCode(t, err, "42501")
		}
	})

	t.Run("success_has_no_incident_and_run_identity_is_unique", func(t *testing.T) {
		id, err := dbfixture.Insert(ctx, api, "success", "pass")
		if err != nil {
			t.Fatal(err)
		}
		_, err = dbfixture.Insert(ctx, api, "success", "pass")
		requireCode(t, err, "23505")
		_, err = api.Exec(dbfixture.IncidentSQL, id)
		requireCode(t, err, "23514")
		var count int
		if err = api.QueryRow("SELECT count(*) FROM evidence.receipts WHERE run_id='success'").Scan(&count); err != nil || count != 1 {
			t.Fatalf("dedup count=%d err=%v", count, err)
		}
	})

	t.Run("concurrent_duplicate_identity_allows_one_insert", func(t *testing.T) {
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { _, err := dbfixture.Insert(ctx, api, "concurrent", "pass"); results <- err }()
		}
		successes := 0
		for i := 0; i < 2; i++ {
			err := <-results
			if err == nil {
				successes++
			} else {
				requireCode(t, err, "23505")
			}
		}
		if successes != 1 {
			t.Fatalf("committed %d duplicates", successes)
		}
	})

	t.Run("failure_and_partial_require_exactly_one_incident_at_commit", func(t *testing.T) {
		for _, verdict := range []string{"fail", "partial"} {
			_, err := dbfixture.Insert(ctx, api, verdict+"-without-incident", verdict)
			requireCode(t, err, "23514")
			tx, err := api.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			id, err := dbfixture.Insert(ctx, tx, verdict+"-paired", verdict)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(dbfixture.IncidentSQL, id); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			_, err = api.Exec(dbfixture.IncidentSQL, id)
			requireCode(t, err, "23505")
			_, err = api.Exec("UPDATE evidence.incidents SET state='resolved' WHERE receipt_id=$1", id)
			requireCode(t, err, "23514")
		}
	})

	t.Run("forced_incident_error_rolls_back_both_records", func(t *testing.T) {
		tx, err := api.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		id, err := dbfixture.Insert(ctx, tx, "rollback", "fail")
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec("INSERT INTO evidence.incidents(receipt_id,issue_key,reason_code,reason) VALUES($1,'rollback','invalid code','test')", id)
		requireCode(t, err, "23514")
		if err = tx.Commit(); err == nil {
			t.Fatal("aborted transaction committed")
		}
		var count int
		if err = api.QueryRow("SELECT count(*) FROM evidence.receipts WHERE run_id='rollback'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback left receipt: %d %v", count, err)
		}
	})

	t.Run("unknown_coverage_untrusted_claims_and_invalid_refs_cannot_pass", func(t *testing.T) {
		cases := []struct{ object, coverage string }{
			{dbfixture.Object(), dbfixture.Coverage("partial")},
			{dbfixture.Object(), strings.ReplaceAll(dbfixture.Coverage("pass"), "trusted", "job")},
			{dbfixture.Object(), `{}`},
			{dbfixture.Object(), `null`},
			{strings.Replace(dbfixture.Object(), strings.Repeat("a", 64), "bad-digest", 1), dbfixture.Coverage("pass")},
			{`{}`, dbfixture.Coverage("pass")},
		}
		for _, item := range cases {
			var id string
			err := api.QueryRow(dbfixture.InsertSQL, "invalid", "pass", item.object, item.coverage).Scan(&id)
			requireCode(t, err, "23514")
		}
		for _, query := range []string{
			strings.Replace(dbfixture.InsertSQL, "jsonb_build_array($3::jsonb)", "'[]'::jsonb", 1),
			strings.Replace(dbfixture.InsertSQL, "'verified'", "'unverified'", 1),
		} {
			var id string
			err := api.QueryRow(query, "invalid", "pass", dbfixture.Object(), dbfixture.Coverage("pass")).Scan(&id)
			requireCode(t, err, "23514")
		}
	})

	t.Run("search_and_completion_indexes_exist_and_text_search_works", func(t *testing.T) {
		var count int
		if err := api.QueryRow("SELECT count(*) FROM evidence.receipts WHERE search_document @@ plainto_tsquery('simple','workflow')").Scan(&count); err != nil || count == 0 {
			t.Fatalf("text search: count=%d %v", count, err)
		}
		if err := admin.QueryRow("SELECT count(*) FROM pg_indexes WHERE schemaname='evidence' AND indexname IN ('receipts_repository_completed','receipts_verdict_completed','receipts_completed','receipts_search','incidents_open_created','finalization_retry_due','finalization_lease_due')").Scan(&count); err != nil || count != 7 {
			t.Fatalf("indexes=%d %v", count, err)
		}
	})

	t.Run("attempts_enforce_retry_schedule_lease_and_receipt_identity", func(t *testing.T) {
		const pending = `INSERT INTO evidence.finalization_attempts(provider,repository_id,run_id,run_attempt,job_id,attempt_number,stage,result)
		 VALUES('local','schema-test-repo','attempt-test',1,'job-1',1,'pending','pending') RETURNING id::text`
		var id string
		if err := api.QueryRow(pending).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var duplicate string
		err := api.QueryRow(pending).Scan(&duplicate)
		requireCode(t, err, "23505")
		_, err = api.Exec("UPDATE evidence.finalization_attempts SET result='running',stage='collect',started_at=now(),lease_owner=gen_random_uuid() WHERE id=$1", id)
		requireCode(t, err, "23514")
		exec(t, api, "UPDATE evidence.finalization_attempts SET result='running',stage='collect',started_at=now(),lease_owner=gen_random_uuid(),lease_expires_at=now()+interval '1 minute' WHERE id=$1", id)
		_, err = api.Exec("UPDATE evidence.finalization_attempts SET result='retry',error_code='s3_unavailable',finished_at=now(),lease_owner=NULL,lease_expires_at=NULL WHERE id=$1", id)
		requireCode(t, err, "23514")
		exec(t, api, "UPDATE evidence.finalization_attempts SET result='retry',error_code='s3_unavailable',finished_at=now(),next_retry_at=now()+interval '1 minute',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1", id)
		wrongReceipt, err := dbfixture.Insert(ctx, api, "different-job", "pass")
		if err != nil {
			t.Fatal(err)
		}
		_, err = api.Exec("UPDATE evidence.finalization_attempts SET result='succeeded',stage='complete',receipt_id=$2,error_code=NULL,next_retry_at=NULL WHERE id=$1", id, wrongReceipt)
		requireCode(t, err, "23503")
		correctReceipt, err := dbfixture.Insert(ctx, api, "attempt-test", "pass")
		if err != nil {
			t.Fatal(err)
		}
		exec(t, api, "UPDATE evidence.finalization_attempts SET result='succeeded',stage='complete',receipt_id=$2,error_code=NULL,next_retry_at=NULL WHERE id=$1", id, correctReceipt)
	})
}
