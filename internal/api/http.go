package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
	"github.com/go-chi/chi/v5"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var regexpRun = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Server struct {
	Store    *Store
	Verifier *Verifier
	Token    string
	slots    chan struct{}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	status := 503
	code := "dependency_unavailable"
	switch {
	case errors.Is(err, ErrInvalid):
		status = 422
		code = "invalid_evidence"
	case errors.Is(err, ErrConflict):
		status = 409
		code = "identity_conflict"
	case errors.Is(err, ErrNotFound):
		status = 404
		code = "not_found"
	}
	if status == 503 {
		w.Header().Set("Retry-After", "60")
	}
	reply(w, status, map[string]string{"error": code})
}
func localHost(host string) bool {
	h, _, e := net.SplitHostPort(host)
	if e != nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1" || h == "api"
}
func (s *Server) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if !localHost(r.Host) {
			reply(w, 403, map[string]string{"error": "host_rejected"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Scheme != "http" || u.Host != r.Host || u.Path != "" {
				reply(w, 403, map[string]string{"error": "origin_rejected"})
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			reply(w, 403, map[string]string{"error": "origin_rejected"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || len(s.Token) < 32 || subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
			reply(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) expensive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "5")
			reply(w, 429, map[string]string{"error": "verification_busy"})
		}
	})
}
func body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "application/json" || r.Header.Get("Content-Encoding") != "" {
		reply(w, 415, map[string]string{"error": "json_required"})
		return nil, false
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if e != nil {
		reply(w, 413, map[string]string{"error": "body_too_large"})
		return nil, false
	}
	return b, true
}
func (s *Server) Handler() http.Handler {
	s.slots = make(chan struct{}, 1)
	r := chi.NewRouter()
	r.Use(s.boundary)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "running"}) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, c := context.WithTimeout(r.Context(), 3*time.Second)
		defer c()
		if s.Store.Pool.Ping(ctx) != nil {
			fail(w, ErrUnavailable)
			return
		}
		reply(w, 200, map[string]string{"status": "ready"})
	})
	r.With(s.auth, s.expensive).Post("/v1/receipts", func(w http.ResponseWriter, r *http.Request) {
		b, ok := body(w, r)
		if !ok {
			return
		}
		result, e := finalizer.ParseResult(b)
		if e != nil {
			fail(w, ErrInvalid)
			return
		}
		v, e := s.Verifier.Verify(r.Context(), *result)
		if e != nil {
			fail(w, e)
			return
		}
		out, e := s.Store.Ingest(r.Context(), v)
		if e != nil {
			fail(w, e)
			return
		}
		status := 201
		if out.Duplicate {
			status = 200
		}
		reply(w, status, out)
	})
	for path, incidents := range map[string]bool{"/v1/receipts": false, "/v1/incidents": true} {
		r.Get(path, func(w http.ResponseWriter, r *http.Request) {
			f, e := ParseFilter(r.URL.Query())
			if e != nil {
				reply(w, 400, map[string]string{"error": "invalid_filter"})
				return
			}
			page, e := s.Store.List(r.Context(), f, incidents)
			if e != nil {
				fail(w, e)
				return
			}
			reply(w, 200, page)
		})
	}
	r.Get("/v1/receipts/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Store.Detail(r.Context(), chi.URLParam(r, "id"))
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, b)
	})
	r.With(s.expensive).Get("/v1/receipts/{id}/verification", func(w http.ResponseWriter, r *http.Request) {
		result, e := s.Store.Result(r.Context(), chi.URLParam(r, "id"))
		if e != nil {
			fail(w, e)
			return
		}
		v, e := s.Verifier.Verify(r.Context(), *result)
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]any{"signature": "verified", "artifacts": "verified", "verdict": v.Receipt.Verdict, "checked_at": time.Now().UTC(), "receipt_sha256": result.Receipt.SHA256})
	})
	r.With(s.auth).Post("/v1/attempts", func(w http.ResponseWriter, r *http.Request) {
		b, ok := body(w, r)
		if !ok {
			return
		}
		var a Attempt
		if strictJSON(b, &a) != nil || a.Identity.Repository != s.Verifier.Repository {
			fail(w, ErrInvalid)
			return
		}
		out, e := s.Store.RecordAttempt(r.Context(), a)
		if e != nil {
			fail(w, e)
			return
		}
		status := 201
		if out.Duplicate {
			status = 200
		}
		reply(w, status, out)
	})
	r.With(s.auth).Get("/v1/attempts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if len(q) > 1 || len(q["run_id"]) > 1 || (len(q) == 1 && !q.Has("run_id")) {
			fail(w, ErrInvalid)
			return
		}
		items, e := s.Store.Attempts(r.Context(), q.Get("run_id"))
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]any{"items": items})
	})
	r.With(s.auth).Post("/v1/recovery", func(w http.ResponseWriter, r *http.Request) {
		b, ok := body(w, r)
		if !ok {
			return
		}
		var a watchdog.Report
		if strictJSON(b, &a) != nil || a.Identity.Repository != s.Verifier.Repository {
			fail(w, ErrInvalid)
			return
		}
		if e := s.Store.Recovery(r.Context(), a); e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]string{"status": "recorded"})
	})
	r.Get("/v1/recovery", func(w http.ResponseWriter, r *http.Request) {
		items, e := s.Store.Recoveries(r.Context())
		if e != nil {
			fail(w, e)
			return
		}
		reply(w, 200, map[string]any{"items": items, "limit": 100})
	})
	return r
}
