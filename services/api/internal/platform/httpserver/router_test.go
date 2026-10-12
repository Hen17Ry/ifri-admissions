package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthAndReadiness(t *testing.T) {
	checks := map[string]Check{"postgres": func(context.Context) error { return errors.New("credential-sensitive failure") }, "redis": func(context.Context) error { return nil }, "minio": func(context.Context) error { return nil }}
	handler := New("http://localhost:5173", checks, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, tc := range []struct {
		path   string
		code   int
		status string
	}{{"/health", 200, "ok"}, {"/ready", 503, "unavailable"}} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if rec.Code != tc.code {
				t.Fatalf("got %d", rec.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != tc.status {
				t.Fatal(body)
			}
			if tc.path == "/ready" && body["checks"].(map[string]any)["postgres"] != "unavailable" {
				t.Fatal(body)
			}
		})
	}
	checks["postgres"] = func(context.Context) error { return nil }
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}
func TestCORS(t *testing.T) {
	h := New("http://localhost:5173", map[string]Check{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, origin := range []string{"http://localhost:5173", "https://attacker.example"} {
		req := httptest.NewRequest("GET", "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if origin == "http://localhost:5173" {
			if rec.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatal("allowed origin missing")
			}
		} else if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("unexpected allowed origin")
		}
	}
	req := httptest.NewRequest("OPTIONS", "/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/health", nil))
	if rec.Code != 405 {
		t.Fatal(rec.Code)
	}
}

func TestReadinessRespectsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := New("http://localhost:5173", map[string]Check{"postgres": func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ready", nil).WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestDraftCORSPreflight(t *testing.T) {
	handler := New("http://localhost:5173", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodOptions, "/api/v1/applications/drafts/current/profile", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	r.Header.Set("Access-Control-Request-Headers", "Content-Type")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Credentials") != "true" || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("draft credentials CORS preflight failed")
	}
}
