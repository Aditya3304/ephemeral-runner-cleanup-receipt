package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBoundaryBeforeDependencies(t *testing.T) {
	s := &Server{Token: strings.Repeat("a", 64)}
	h := s.Handler()
	for _, tc := range []struct {
		name, method, path, host, origin, token, body string
		want                                          int
	}{
		{name: "health", method: "GET", path: "/healthz", host: "localhost:8080", want: 200},
		{name: "rebind", method: "GET", path: "/healthz", host: "evil.example", want: 403},
		{name: "cross origin", method: "GET", path: "/healthz", host: "localhost:8080", origin: "http://evil.example", want: 403},
		{name: "ingest unauthorized", method: "POST", path: "/v1/receipts", host: "localhost:8080", want: 401},
		{name: "attempt unauthorized", method: "POST", path: "/v1/attempts", host: "api:8080", want: 401},
		{name: "oversized", method: "POST", path: "/v1/receipts", host: "api:8080", token: strings.Repeat("a", 64), body: strings.Repeat("x", 17000), want: 413},
		{name: "malformed", method: "POST", path: "/v1/receipts", host: "api:8080", token: strings.Repeat("a", 64), body: `{}`, want: 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing boundary headers")
			}
		})
	}
}
func TestFilters(t *testing.T) {
	for _, q := range []string{"limit=101", "limit=0", "limit=2&limit=3", "verdict=good", "from=tomorrow", "cursor=broken", "unknown=1", "from=2026-09-07T10:00:00Z&to=2026-09-06T10:00:00Z"} {
		v, _ := url.ParseQuery(q)
		if _, e := ParseFilter(v); e == nil {
			t.Fatalf("accepted %s", q)
		}
	}
	v, _ := url.ParseQuery("limit=3&verdict=fail&incident_state=open&q=hello&from=2026-09-07T10:00:00Z")
	if _, e := ParseFilter(v); e != nil {
		t.Fatal(e)
	}
}
func TestStrictJSON(t *testing.T) {
	var c Config
	for _, b := range []string{`{"Repository":"x"}`, `{"repository":"x","repository":"y"}`, `null`, `{} garbage`} {
		if strictJSON([]byte(b), &c) == nil {
			t.Fatalf("accepted %s", b)
		}
	}
}
