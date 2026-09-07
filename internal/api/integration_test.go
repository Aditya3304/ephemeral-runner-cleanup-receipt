package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	schema "github.com/Aditya3304/ephemeral-runner-cleanup-receipt/db"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/database"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isolatedStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	root, e := database.Open(ctx, "proof", true)
	if e != nil {
		t.Fatal(e)
	}
	random := make([]byte, 8)
	if _, e = rand.Read(random); e != nil {
		t.Fatal(e)
	}
	name := "proofcheck_api_" + hex.EncodeToString(random)
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = root.Exec("CREATE DATABASE " + quoted); e != nil {
		t.Fatal(e)
	}
	var admin *sql.DB
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if admin != nil {
			admin.Close()
		}
		if _, e := root.Exec("DROP DATABASE " + quoted + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
		root.Close()
	})
	admin, e = database.Open(ctx, name, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = schema.Migrate(ctx, admin, "up"); e != nil {
		t.Fatal(e)
	}
	if _, e = root.Exec("REVOKE ALL ON DATABASE " + quoted + " FROM PUBLIC; GRANT CONNECT ON DATABASE " + quoted + " TO proof_api"); e != nil {
		t.Fatal(e)
	}
	cfg, e := database.Config(name, false)
	if e != nil {
		t.Fatal(e)
	}
	pc, e := pgxpool.ParseConfig("")
	if e != nil {
		t.Fatal(e)
	}
	pc.ConnConfig = cfg
	pc.MaxConns = 4
	pool, e = pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	if e = pool.Ping(ctx); e != nil {
		t.Fatal(e)
	}
	return &Store{Pool: pool}, admin
}

type memoryArchive map[string][]byte

func (m memoryArchive) Read(ctx context.Context, r archive.Ref) ([]byte, error) {
	b, ok := m[r.SHA256]
	if !ok {
		return nil, errors.New("unavailable")
	}
	return b, nil
}
func TestRealIngestion(t *testing.T) {
	if os.Getenv("PROOF_API_REAL") != "1" {
		t.Skip("requires isolated local services and genuine signed fixtures")
	}
	c, e := LoadConfig("/config/api.json")
	if e != nil {
		t.Fatal(e)
	}
	ac, e := archive.LoadConfig(c.ArchiveConfig)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := archive.New(ac)
	if e != nil {
		t.Fatal(e)
	}
	signer := &finalizer.Cosign{Binary: c.CosignBinary, SigningConfig: c.SigningConfig, TrustRoot: c.TrustRoot, Policy: c.Policy}
	v := &Verifier{Archive: reader, Signature: signer, Policy: c.Policy, Repository: c.Repository}
	ctx := context.Background()
	paths, e := filepath.Glob("/fixtures/*/result.json")
	if e != nil {
		t.Fatal(e)
	}
	var pass, fail *Verified
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		r, e := finalizer.ParseResult(b)
		if e != nil {
			continue
		}
		if r.Identity.Revision != c.Policy.Revision {
			continue
		}
		got, e := v.Verify(ctx, *r)
		if e != nil {
			t.Fatalf("real verify %s: %v", r.Identity.Run, e)
		}
		if got.Receipt.Verdict == "pass" && pass == nil {
			pass = got
		}
		if got.Receipt.Verdict == "fail" {
			fail = got
		}
	}
	if pass == nil || fail == nil {
		t.Fatal("need genuine pass/fail fixtures")
	}
	t.Run("real_cosign_rejects_tampering_and_wrong_policy", func(t *testing.T) {
		raw, e := reader.Read(ctx, pass.Result.Receipt)
		if e != nil {
			t.Fatal(e)
		}
		bundle, e := reader.Read(ctx, pass.Result.Bundle)
		if e != nil {
			t.Fatal(e)
		}
		m := memoryArchive{pass.Result.Receipt.SHA256: raw, pass.Result.Bundle.SHA256: bundle}
		clone := *v
		clone.Archive = m
		changed := pass.Receipt
		changed.FinalizedAt = changed.FinalizedAt.Add(time.Second)
		bad, _ := changed.Canonical()
		ref := pass.Result
		ref.Receipt.SHA256 = proof.Digest(bad)
		ref.Receipt.Key = "evidence/sha256/" + ref.Receipt.SHA256
		ref.Receipt.SizeBytes = int64(len(bad))
		m[ref.Receipt.SHA256] = bad
		if _, e = clone.Verify(ctx, ref); !errors.Is(e, ErrInvalid) {
			t.Fatalf("modified signed bytes accepted: %v", e)
		}
		ref = pass.Result
		bad = append([]byte(nil), bundle...)
		bad[len(bad)/2] ^= 1
		ref.Bundle.SHA256 = proof.Digest(bad)
		ref.Bundle.Key = "evidence/sha256/" + ref.Bundle.SHA256
		ref.Bundle.SizeBytes = int64(len(bad))
		m[ref.Bundle.SHA256] = bad
		if _, e = clone.Verify(ctx, ref); !errors.Is(e, ErrInvalid) {
			t.Fatalf("modified bundle accepted: %v", e)
		}
		for _, field := range []string{"issuer", "identity", "revision", "root"} {
			p := c.Policy
			switch field {
			case "issuer":
				p.Issuer = "https://wrong.invalid"
			case "identity":
				p.Identity = "wrong@cleanup-receipt.local"
			case "revision":
				p.Revision = strings.Repeat("0", 40)
			case "root":
				p.TrustRootSHA256 = strings.Repeat("0", 64)
			}
			clone.Policy = p
			if _, e = clone.Verify(ctx, pass.Result); !errors.Is(e, ErrInvalid) {
				t.Fatal("wrong operator policy accepted")
			}
		}
		for _, change := range []string{"issuer", "identity"} {
			s := *signer
			if change == "issuer" {
				s.Policy.Issuer = "https://wrong.invalid"
			} else {
				s.Policy.Identity = "wrong@example.invalid"
			}
			if s.Verify(ctx, raw, bundle) == nil {
				t.Fatal("wrong signer accepted")
			}
		}
	})
	store, admin := isolatedStore(t)
	t.Run("concurrent_dedup_incident_and_conflict", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan Commit, 12)
		errs := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r, e := store.Ingest(ctx, fail); results <- r; errs <- e }()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		created := 0
		id := ""
		for r := range results {
			if !r.Duplicate {
				created++
			}
			if id != "" && id != r.ID {
				t.Fatal("different duplicate IDs")
			}
			id = r.ID
		}
		if created != 1 {
			t.Fatalf("created %d", created)
		}
		var n int
		if e := admin.QueryRow("SELECT count(*) FROM evidence.incidents").Scan(&n); e != nil || n != 1 {
			t.Fatalf("incidents %d: %v", n, e)
		}
		clone := *fail
		clone.Result.Bundle.VersionID = "another-version"
		if _, e := store.Ingest(ctx, &clone); !errors.Is(e, ErrConflict) {
			t.Fatal("accepted conflicting immutable reference")
		}
	})
	t.Run("forced_incident_failure_rolls_back_receipt", func(t *testing.T) {
		clone := *fail
		clone.Receipt.Identity.Run = strings.Repeat("f", 32)
		clone.Result.Identity = clone.Receipt.Identity
		if _, e := admin.Exec(`CREATE FUNCTION evidence.api_test_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced incident failure'; END $$; CREATE TRIGGER api_test_reject BEFORE INSERT ON evidence.incidents FOR EACH ROW EXECUTE FUNCTION evidence.api_test_reject()`); e != nil {
			t.Fatal(e)
		}
		_, e := store.Ingest(ctx, &clone)
		if e == nil {
			t.Fatal("forced transaction unexpectedly succeeded")
		}
		var n int
		if e = admin.QueryRow("SELECT count(*) FROM evidence.receipts WHERE run_id=$1", clone.Receipt.Identity.Run).Scan(&n); e != nil || n != 0 {
			t.Fatal("orphan receipt survived rollback")
		}
		if _, e = admin.Exec("DROP TRIGGER api_test_reject ON evidence.incidents; DROP FUNCTION evidence.api_test_reject()"); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("sanitization_queries_and_keyset", func(t *testing.T) {
		clone := *pass
		clone.Receipt.Coverage = map[string]proof.Observation{}
		for k, o := range pass.Receipt.Coverage {
			o.Reason = "password=DO_NOT_STORE <script>evil()</script>"
			clone.Receipt.Coverage[k] = o
		}
		result, e := store.Ingest(ctx, &clone)
		if e != nil {
			t.Fatal(e)
		}
		b, e := store.Detail(ctx, result.ID)
		if e != nil || strings.Contains(string(b), "DO_NOT_STORE") || strings.Contains(string(b), "<script>") {
			t.Fatal("raw metadata leaked")
		}
		if _, e = store.Result(ctx, result.ID); e != nil {
			t.Fatal(e)
		}
		for _, f := range []Filter{{Limit: 25, Verdict: "pass"}, {Limit: 25, Verdict: "fail", Incident: "open"}, {Limit: 25, Search: pass.Receipt.Identity.Job}, {Limit: 25, Repository: c.Repository}, {Limit: 25, From: pass.Receipt.CompletedAt.Add(-time.Hour), To: pass.Receipt.CompletedAt.Add(time.Hour)}} {
			page, e := store.List(ctx, f, false)
			if e != nil || len(page.Items) == 0 {
				t.Fatalf("filter failed: %+v %v", f, e)
			}
		}
		page, e := store.List(ctx, Filter{Limit: 1}, false)
		if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
			t.Fatal("missing cursor")
		}
		f, e := ParseFilter(map[string][]string{"cursor": {page.NextCursor}, "limit": {"1"}})
		if e != nil {
			t.Fatal(e)
		}
		next, e := store.List(ctx, f, false)
		if e != nil || len(next.Items) != 1 || string(next.Items[0]) == string(page.Items[0]) {
			t.Fatal("pagination failed")
		}
	})
	t.Run("attempt_auth_contract_and_db_privileges", func(t *testing.T) {
		due := time.Now().Add(time.Minute)
		a := Attempt{Identity: pass.Receipt.Identity, Number: 1, Stage: "ingest", Result: "retry", ErrorCode: "dependency_unavailable", NextRetryAt: &due}
		first, e := store.RecordAttempt(ctx, a)
		if e != nil {
			t.Fatal(e)
		}
		again, e := store.RecordAttempt(ctx, a)
		if e != nil || !again.Duplicate || first.ID != again.ID {
			t.Fatal("attempt replay failed")
		}
		a.ErrorCode = "invalid_evidence"
		if _, e = store.RecordAttempt(ctx, a); !errors.Is(e, ErrConflict) {
			t.Fatal("attempt overwritten")
		}
		items, e := store.Attempts(ctx, pass.Receipt.Identity.Run)
		if e != nil || len(items) != 1 {
			t.Fatal("attempt query failed")
		}
		for _, query := range []string{"DELETE FROM evidence.receipts", "UPDATE evidence.receipts SET verdict='pass'", "CREATE TABLE evidence.unauthorized(id int)", "DELETE FROM evidence.incidents"} {
			if _, e = store.Pool.Exec(ctx, query); e == nil {
				t.Fatalf("API role allowed %s", query)
			}
		}
		var tls bool
		if e = store.Pool.QueryRow(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&tls); e != nil || !tls {
			t.Fatal("database not TLS")
		}
	})
}
