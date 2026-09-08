package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	schema "github.com/Aditya3304/ephemeral-runner-cleanup-receipt/db"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubrun"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// This opt-in test is run inside a network-none PostgreSQL container with a
// fresh tmpfs cluster. It does not use the operator database or production keys.
func TestGitHubHTTPDatabase(t *testing.T) {
	if os.Getenv("PROOF_GITHUB_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable loopback PostgreSQL test cluster")
	}
	ctx := context.Background()
	dsn := "postgres://postgres@127.0.0.1:5432/"
	admin, err := sql.Open("pgx", dsn+"postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "github_test_" + strings.ReplaceAll(watchdog.UUID(), "-", "")
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
	db, err := sql.Open("pgx", dsn+name+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = schema.Migrate(ctx, db, "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, "postgres://proof_api@127.0.0.1:5432/"+name+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := &Store{Pool: pool}
	e, v, id, objects, signer := githubFixture(t, "normal")
	result, err := e.Run(ctx, githubrun.Directory(id))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, Verifier: v, Token: strings.Repeat("t", 64)}
	handler := s.Handler()
	request := func(method, path string, value any) *httptest.ResponseRecorder {
		var b []byte
		if value != nil {
			b, _ = proof.Canonical(value)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Host = "api:8080"
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	first := request(http.MethodPost, "/v1/receipts", result)
	if first.Code != 201 {
		t.Fatalf("ingest HTTP %d: %s", first.Code, first.Body.String())
	}
	var commit Commit
	if err = json.Unmarshal(first.Body.Bytes(), &commit); err != nil {
		t.Fatal(err)
	}
	again := request(http.MethodPost, "/v1/receipts", result)
	if again.Code != 200 {
		t.Fatalf("duplicate HTTP %d: %s", again.Code, again.Body.String())
	}
	var duplicate Commit
	json.Unmarshal(again.Body.Bytes(), &duplicate)
	if duplicate.ID != commit.ID || !duplicate.Duplicate {
		t.Fatal("duplicate receipt created")
	}
	var count, incidents int
	var workflow, provider string
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM evidence.receipts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM evidence.incidents").Scan(&incidents); err != nil {
		t.Fatal(err)
	}
	if count != 1 || incidents != 1 {
		t.Fatalf("receipt/incident duplication %d/%d", count, incidents)
	}
	if err = pool.QueryRow(ctx, "SELECT workflow,provider FROM evidence.receipts WHERE id=$1", commit.ID).Scan(&workflow, &provider); err != nil {
		t.Fatal(err)
	}
	if workflow != "GitHub Actions" || provider != "github" {
		t.Fatal("local-only metadata remains")
	}
	report := watchdog.Report{Identity: id, Number: 1, Stage: "complete", Status: "succeeded", ReceiptID: commit.ID}
	if out := request(http.MethodPost, "/v1/recovery", report); out.Code != 200 {
		t.Fatalf("recovery HTTP %d: %s", out.Code, out.Body.String())
	}
	report.Identity.Attempt++
	if out := request(http.MethodPost, "/v1/recovery", report); out.Code != 422 {
		t.Fatal("recovery linked receipt to wrong attempt")
	}
	if out := request(http.MethodGet, "/v1/receipts?run_id="+id.Run, nil); out.Code != 200 || !strings.Contains(out.Body.String(), commit.ID) {
		t.Fatal("GitHub receipt not visible in dashboard API")
	}
	changed := *result
	changed.Identity.Attempt++
	if out := request(http.MethodPost, "/v1/receipts", changed); out.Code != 422 {
		t.Fatal("unverified rerun accepted")
	}
	secondEngine, _, secondID, _, _ := githubFixture(t, "normal", 2)
	secondEngine.Archive = objects
	secondEngine.Signer = signer
	second, err := secondEngine.Run(ctx, githubrun.Directory(secondID))
	if err != nil {
		t.Fatal(err)
	}
	if out := request(http.MethodPost, "/v1/receipts", second); out.Code != 201 {
		t.Fatalf("verified rerun HTTP %d: %s", out.Code, out.Body.String())
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM evidence.receipts WHERE provider='github' AND run_id=$1", id.Run).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rerun did not get its own receipt: %d %v", count, err)
	}
}
