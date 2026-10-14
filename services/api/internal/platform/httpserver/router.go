package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Check func(context.Context) error

func New(origin string, checks map[string]Check, logger *slog.Logger, register ...func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	for _, add := range register {
		add(mux)
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		results := make(map[string]string, len(checks))
		type result struct {
			name string
			err  error
		}
		done := make(chan result, len(checks))
		for name, check := range checks {
			results[name] = "unavailable"
			go func() { done <- result{name, check(ctx)} }()
		}
		status, code := "ok", http.StatusOK
	checksLoop:
		for range checks {
			select {
			case res := <-done:
				if res.err == nil {
					results[res.name] = "ok"
				} else {
					status, code = "unavailable", http.StatusServiceUnavailable
				}
			case <-ctx.Done():
				status, code = "unavailable", http.StatusServiceUnavailable
				break checksLoop
			}
		}
		respond(w, code, struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		}{status, results})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Origin")
		if requestedOrigin := r.Header.Get("Origin"); requestedOrigin != "" {
			if requestedOrigin != origin {
				respond(w, http.StatusForbidden, map[string]string{"status": "forbidden"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == http.MethodOptions {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				method := r.Header.Get("Access-Control-Request-Method")
				draftMutation := method == http.MethodPost && (r.URL.Path == "/api/v1/applications/drafts" || strings.HasPrefix(r.URL.Path, "/api/v1/applications/drafts/current/documents/") || r.URL.Path == "/api/v1/applications/drafts/current/transcript-analysis/confirm" || r.URL.Path == "/api/v1/applications/drafts/current/transcript-analysis/reject" || r.URL.Path == "/api/v1/applications/drafts/current/submission")
				if method == http.MethodPatch {
					switch r.URL.Path {
					case "/api/v1/applications/drafts/current/profile", "/api/v1/applications/drafts/current/academic-background", "/api/v1/applications/drafts/current/program":
						draftMutation = true
					}
				}
				if (method != http.MethodGet && !draftMutation) || (r.URL.Path != "/health" && r.URL.Path != "/ready" && !strings.HasPrefix(r.URL.Path, "/api/v1/")) {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Application-Version")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		mux.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "route", r.Pattern, "duration_ms", time.Since(started).Milliseconds())
	})
}
func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
