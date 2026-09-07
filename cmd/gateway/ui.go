package main

import (
	"bytes"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

// Only build assets are served. No directory listing, arbitrary upstream,
// dotfile access, or SPA fallback for a missing API endpoint is permitted.
func gatewayHandler(api http.Handler, assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "api" {
			http.Error(w, "Host rejected", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/readyz" || r.URL.Path == "/healthz" || r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		allowed := name == "index.html" || name == "favicon.svg" || (strings.HasPrefix(name, "assets/") && strings.Count(name, "/") == 1)
		if !allowed || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		data, e := fs.ReadFile(assets, name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		typ := mime.TypeByExtension(path.Ext(name))
		if typ != "" {
			w.Header().Set("Content-Type", typ)
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
