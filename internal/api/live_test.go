package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func TestLiveHTTP(t *testing.T) {
	if os.Getenv("PROOF_API_HTTP") != "1" {
		t.Skip("requires running API and authenticated test client")
	}
	secret, e := os.ReadFile("/run/auth/token")
	if e != nil {
		t.Fatal(e)
	}
	token := strings.TrimSpace(string(secret))
	call := func(method, path string, b []byte, auth bool) (int, []byte) {
		t.Helper()
		r, e := http.NewRequest(method, "http://api:8080"+path, bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		client := http.Client{Timeout: 50 * time.Second}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, raw
	}
	paths, _ := filepath.Glob("/fixtures/*/result.json")
	var result *finalizer.Result
	for _, p := range paths {
		b, _ := os.ReadFile(p)
		r, e := finalizer.ParseResult(b)
		if e == nil && r.Identity.Run == "2687c9f75993ac1041d1a8b4e89ded21" {
			result = r
			break
		}
	}
	if result == nil {
		t.Fatal("genuine milestone d fixture missing")
	}
	canonical, _ := proof.Canonical(result)
	status, b := call("POST", "/v1/receipts", canonical, true)
	if status != 200 {
		t.Fatalf("duplicate status %d: %s", status, b)
	}
	var committed Commit
	if json.Unmarshal(b, &committed) != nil || !committed.Duplicate {
		t.Fatal("duplicate result missing")
	}
	for _, tc := range []struct {
		name string
		data []byte
		auth bool
		want int
	}{
		{"unauthorized", canonical, false, 401},
		{"oversized", bytes.Repeat([]byte("x"), 17000), true, 413},
		{"noncanonical", append([]byte(" "), canonical...), true, 422},
		{"caller verification claim", []byte(`{"signature_state":"verified"}`), true, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, b := call("POST", "/v1/receipts", tc.data, tc.auth)
			if status != tc.want {
				t.Fatalf("status %d: %s", status, b)
			}
		})
	}
	changed := *result
	changed.Identity.Job = "another-job"
	bad, _ := proof.Canonical(changed)
	if code, b := call("POST", "/v1/receipts", bad, true); code != 422 {
		t.Fatalf("identity tamper %d: %s", code, b)
	}
	code, b := call("GET", "/v1/receipts/"+committed.ID+"/verification", nil, false)
	if code != 200 || !bytes.Contains(b, []byte(`"artifacts":"verified"`)) {
		t.Fatalf("independent verification %d: %s", code, b)
	}
	for path, want := range map[string]int{"/v1/receipts?verdict=fail&incident_state=open": 200, "/v1/incidents": 200, "/v1/receipts?limit=101": 400, "/v1/attempts": 401} {
		code, b := call("GET", path, nil, false)
		if code != want {
			t.Fatalf("query %s: %d %s", path, code, b)
		}
	}
	code, b = call("GET", "/v1/attempts", nil, true)
	if code != 200 {
		t.Fatalf("attempt query %d: %s", code, b)
	}
}

func TestAPINetworkIsolation(t *testing.T) {
	if os.Getenv("PROOF_API_NETWORK") != "1" {
		t.Skip("requires API network namespace")
	}
	for _, target := range []string{"db:5432", "minio:9000"} {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		conn, e := (&net.Dialer{}).DialContext(ctx, "tcp", target)
		c()
		if e != nil {
			t.Fatalf("positive control %s: %v", target, e)
		}
		conn.Close()
	}
	for _, target := range []string{"issuer:8443", "fulcio:5555", "rekor:3000", "1.1.1.1:443"} {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		conn, e := (&net.Dialer{}).DialContext(ctx, "tcp", target)
		c()
		if e == nil {
			conn.Close()
			t.Fatalf("unexpected reachability %s", target)
		}
	}
}
