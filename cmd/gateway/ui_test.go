package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUIBoundary(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>AFTER</title>")}, "assets/app.js": {Data: []byte("export {}")}, "assets/font.woff2": {Data: []byte("font")}, "private.json": {Data: []byte("not exposed")}}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-API", "true"); w.WriteHeader(202) })
	handler := gatewayHandler(api, assets)
	for _, tc := range []struct {
		method, path, host string
		want               int
	}{{"GET", "/", "localhost:8080", 200}, {"GET", "/?receipt=demo", "127.0.0.1:8080", 200}, {"GET", "/assets/app.js", "localhost", 200}, {"HEAD", "/assets/font.woff2", "localhost", 200}, {"GET", "/assets/missing.js", "localhost", 404}, {"GET", "/assets/../private.json", "localhost", 404}, {"GET", "/private.json", "localhost", 404}, {"GET", "/assets/", "localhost", 404}, {"POST", "/", "localhost", 405}, {"GET", "/", "evil.example", 403}, {"POST", "/v1/receipts", "localhost", 202}, {"GET", "/readyz", "localhost", 202}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = tc.host
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			if tc.want == 200 && !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
				t.Fatal("missing local CSP")
			}
			if strings.Contains(tc.path, ".js") && tc.want == 200 && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("missing asset cache policy")
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD returned bytes")
			}
		})
	}
}
