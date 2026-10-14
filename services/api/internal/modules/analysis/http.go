package analysis

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/platform/storage"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

type Handler struct {
	Logger         *slog.Logger
	Ready          func(context.Context) error
	Repo           Repository
	Origin, Secret string
	Objects        storage.Objects
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	var p *drafts.Problem
	if !errors.As(err, &p) {
		p = &drafts.Problem{Status: 500, Code: "ANALYSIS_UNAVAILABLE", Message: "L’analyse est momentanément indisponible. Réessayez."}
	}
	send(w, p.Status, map[string]any{"error": p})
}
func session(r *http.Request) ([]byte, error) {
	c, err := r.Cookie(drafts.CookieName)
	if err != nil || !drafts.ValidSecret(c.Value) {
		return nil, drafts.ErrSession
	}
	return drafts.HashSecret(c.Value), nil
}
func (h Handler) RegisterPublic(m *http.ServeMux) {
	base := drafts.CookiePath + "/current/transcript-analysis"
	m.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		hash, err := session(r)
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		v, err := h.Repo.Current(ctx, hash)
		if err != nil {
			fail(w, err)
			return
		}
		send(w, 200, v)
	})
	for _, action := range []string{"confirm", "reject"} {
		m.HandleFunc("POST "+base+"/"+action, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Origin") != h.Origin {
				fail(w, &drafts.Problem{Status: 403, Code: "ORIGIN_FORBIDDEN", Message: "Origine non autorisée."})
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				fail(w, &drafts.Problem{Status: 415, Code: "JSON_REQUIRED", Message: "Une requête JSON est requise."})
				return
			}
			hash, err := session(r)
			if err != nil {
				fail(w, err)
				return
			}
			var input struct {
				AnalysisID string `json:"analysisId"`
				Version    int64  `json:"version"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			dec.DisallowUnknownFields()
			if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
				fail(w, &drafts.Problem{Status: 400, Code: "INVALID_PARAMETERS", Message: "Requête invalide."})
				return
			}
			decision := "CONFIRMED"
			if action == "reject" {
				decision = "REJECTED"
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			v, err := h.Repo.Decide(ctx, hash, input.AnalysisID, input.Version, decision)
			if err != nil {
				fail(w, err)
				return
			}
			send(w, 200, v)
		})
	}
}
func (h Handler) RegisterInternal(m *http.ServeMux) {
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Secret)) != 1 {
				w.WriteHeader(401)
				return
			}
			next(w, r)
		}
	}
	m.HandleFunc("GET /internal/ocr/ready", auth(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		check := h.Ready
		if check == nil {
			check = h.Repo.Pool.Ping
		}
		if err := check(ctx); err != nil {
			w.WriteHeader(503)
			return
		}
		send(w, 200, map[string]string{"status": "ok"})
	}))
	m.HandleFunc("POST /internal/ocr/{id}/result", auth(func(w http.ResponseWriter, r *http.Request) {
		var result Result
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
		dec.DisallowUnknownFields()
		if dec.Decode(&result) != nil || dec.Decode(new(any)) != io.EOF {
			w.WriteHeader(400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := h.Repo.Complete(ctx, r.PathValue("id"), result); err != nil {
			w.WriteHeader(422)
			return
		}
		if h.Logger != nil {
			h.Logger.Info("OCR result acknowledged", "analysis_id", r.PathValue("id"), "engine", result.Engine, "engine_version", result.EngineVersion, "page_count", result.Pages, "reported_status", result.Status, "failure_code", result.Failure, "attempt_number", result.Attempt)
		}
		send(w, 200, map[string]string{"status": "accepted"})
	}))
	m.HandleFunc("POST /internal/ocr/{id}/claim", auth(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var status string
		var attempt int
		err := h.Repo.Pool.QueryRow(ctx, `UPDATE ocr_analyses SET status='PROCESSING',attempt_number=attempt_number+1,started_at=now(),updated_at=now() WHERE id=$1::uuid AND result_digest IS NULL AND (status='QUEUED' OR (status='PROCESSING' AND started_at<now()-interval '5 minutes')) AND attempt_number<3 RETURNING status,attempt_number`, r.PathValue("id")).Scan(&status, &attempt)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				w.WriteHeader(503)
				return
			}
			send(w, 200, map[string]string{"status": "SKIP"})
			return
		}
		send(w, 200, map[string]any{"status": status, "attemptNumber": attempt})
	}))
	m.HandleFunc("GET /internal/ocr/{id}/document", auth(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var key string
		err := h.Repo.Pool.QueryRow(ctx, `SELECT v.storage_key FROM ocr_analyses a JOIN document_versions v ON v.id=a.document_version_id WHERE a.id=$1::uuid AND a.status='PROCESSING'`, r.PathValue("id")).Scan(&key)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		obj, err := h.Objects.Get(ctx, key)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer obj.Body.Close()
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.Copy(w, obj.Body)
	}))
}
