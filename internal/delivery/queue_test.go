package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func fixture() ([]byte, finalizer.Result) {
	ref := archive.Ref{Store: "local-minio", Bucket: "receipt-archive", Key: "evidence/sha256/" + strings.Repeat("a", 64), SHA256: strings.Repeat("a", 64), VersionID: "fixture-version", SizeBytes: 1}
	r := finalizer.Result{Kind: "cleanup-finalization-result/v1", Identity: proof.Identity{Provider: "local", Repository: "owner/repo", Run: strings.Repeat("b", 32), Attempt: 1, Job: "job", Revision: strings.Repeat("c", 40)}, Receipt: ref, Bundle: ref, SignatureState: "verified"}
	b, _ := proof.Canonical(r)
	return b, r
}
func TestOutageRestartAndAmbiguousCommit(t *testing.T) {
	b, result := fixture()
	up := false
	committed := false
	posts, reports := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/v1/attempts" {
			reports++
			w.WriteHeader(201)
			return
		}
		if r.Method == "GET" {
			items := []any{}
			if committed {
				items = append(items, map[string]any{"id": "db-id", "receipt_object": result.Receipt, "bundle_object": result.Bundle})
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items})
			return
		}
		posts++
		committed = true
		w.WriteHeader(503)
	}))
	defer server.Close()
	now := time.Now()
	dir := t.TempDir()
	q := &Queue{Dir: dir, BaseURL: server.URL, Client: server.Client(), Now: func() time.Time { return now }}
	s, e := q.Deliver(context.Background(), b)
	if e == nil || s.Status != "retry" || s.Tries != 1 || s.NextRetryAt == nil {
		t.Fatal("outage not persisted")
	}
	up = true
	q = &Queue{Dir: dir, BaseURL: server.URL, Client: server.Client(), Now: func() time.Time { return now }}
	s, e = q.Deliver(context.Background(), b)
	if e == nil || posts != 0 {
		t.Fatal("retry ran early")
	}
	now = now.Add(2 * time.Minute)
	s, e = q.Deliver(context.Background(), b)
	if e == nil || posts != 1 {
		t.Fatal("ambiguous commit not recorded")
	}
	s, e = q.Deliver(context.Background(), b)
	if e != nil || s.Status != "complete" || s.ReceiptID != "db-id" || posts != 1 || reports == 0 {
		t.Fatal("committed retry not reconciled")
	}
	result.Bundle.VersionID = "changed"
	changed, _ := proof.Canonical(result)
	if _, e = q.Deliver(context.Background(), changed); e == nil {
		t.Fatal("accepted changed immutable input")
	}
}
func TestSixAttemptCap(t *testing.T) {
	b, _ := fixture()
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/receipts" {
			posts++
		}
		w.WriteHeader(503)
	}))
	defer server.Close()
	now := time.Now()
	q := &Queue{Dir: t.TempDir(), BaseURL: server.URL, Client: server.Client(), Now: func() time.Time { return now }}
	for i := 1; i <= 7; i++ {
		s, e := q.Deliver(context.Background(), b)
		if e == nil {
			t.Fatal("outage succeeded")
		}
		if i >= 6 && s.Status != "exhausted" {
			t.Fatal("budget not exhausted")
		}
		now = now.Add(2 * time.Hour)
	}
	if posts != 6 {
		t.Fatalf("got %d delivery attempts", posts)
	}
}
