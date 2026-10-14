package submissions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"ifri-admissions/services/api/internal/modules/drafts"
)

// Service is the submission use case consumed by the HTTP adapter.
type Service interface {
	Submit(context.Context, []byte, int64) (Receipt, Outcome, error)
	Receipt(context.Context, []byte) (Receipt, error)
}

type Handler struct {
	service Service
	origin  string
	secure  bool
	logger  *slog.Logger
}

// Register mounts the routes under the draft cookie path, the only path the cookie is sent to.
func Register(mux *http.ServeMux, service Service, origin string, secure bool, logger *slog.Logger) {
	h := Handler{service, origin, secure, logger}
	path := drafts.CookiePath + "/current/submission"
	mux.HandleFunc("POST "+path, h.submit)
	mux.HandleFunc("GET "+path, h.receipt)
}

func (h Handler) send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h Handler) fail(w http.ResponseWriter, err error) {
	var p *drafts.Problem
	if !errors.As(err, &p) {
		p = &drafts.Problem{Status: 500, Code: "SUBMISSION_FAILED", Message: "La soumission n’a pas pu être enregistrée. Réessayez : tant qu’aucun numéro n’est affiché, votre candidature reste un brouillon."}
		h.logger.Error("application submission failed")
	}
	if p.Status == 401 {
		http.SetCookie(w, &http.Cookie{Name: drafts.CookieName, Path: drafts.CookiePath, MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	}
	h.send(w, p.Status, map[string]any{"error": p})
}

func session(r *http.Request) ([]byte, string, error) {
	c, err := r.Cookie(drafts.CookieName)
	if err != nil || !drafts.ValidSecret(c.Value) {
		return nil, "", drafts.ErrSession
	}
	return drafts.HashSecret(c.Value), c.Value, nil
}

func (h Handler) submit(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != h.origin {
		h.fail(w, &drafts.Problem{Status: 403, Code: "ORIGIN_FORBIDDEN", Message: "Cette origine n’est pas autorisée."})
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		h.fail(w, &drafts.Problem{Status: 415, Code: "JSON_REQUIRED", Message: "Une requête JSON est requise."})
		return
	}
	hash, token, err := session(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF || input.Version < 1 {
		h.fail(w, &drafts.Problem{Status: 400, Code: "INVALID_PARAMETERS", Message: "Le contenu de la requête est invalide."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	receipt, out, err := h.service.Submit(ctx, hash, input.Version)
	if err != nil {
		h.fail(w, err)
		return
	}
	// Same response for the first request, a double click or a retry; only technical ids logged.
	h.logger.Info("application submitted", "application_id", out.ApplicationID, "submission_id", out.SubmissionID, "replay", out.Replay)
	http.SetCookie(w, &http.Cookie{Name: drafts.CookieName, Value: token, Path: drafts.CookiePath, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(receipt.SessionExpiresAt).Seconds()), Expires: receipt.SessionExpiresAt})
	h.send(w, 200, receipt)
}

func (h Handler) receipt(w http.ResponseWriter, r *http.Request) {
	hash, _, err := session(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	receipt, err := h.service.Receipt(ctx, hash)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, receipt)
}
