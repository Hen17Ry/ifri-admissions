package drafts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"ifri-admissions/services/api/internal/modules/references"
)

type Handler struct {
	repository Repository
	ttl        time.Duration
	origin     string
	secure     bool
	logger     *slog.Logger
	limiter    *createLimiter
}

func Register(mux *http.ServeMux, repository Repository, ttl time.Duration, origin string, secure bool, logger *slog.Logger) {
	h := Handler{repository: repository, ttl: ttl, origin: origin, secure: secure, logger: logger, limiter: &createLimiter{entries: map[string]window{}}}
	mux.HandleFunc("POST "+CookiePath, h.create)
	mux.HandleFunc("GET "+CookiePath+"/current", h.current)
	for _, kind := range []string{"profile", "academic-background", "program"} {
		mux.HandleFunc("PATCH "+CookiePath+"/current/"+kind, h.update)
	}
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (h Handler) failure(w http.ResponseWriter, err error) {
	var p *Problem
	if !errors.As(err, &p) {
		switch {
		case errors.Is(err, references.ErrNoCurrentCampaign), errors.Is(err, references.ErrCampaignNotOpen):
			p = ErrClosed
		case errors.Is(err, references.ErrAmbiguousCampaign):
			p = problem(409, "AMBIGUOUS_CURRENT_CAMPAIGN", "Plusieurs campagnes sont ouvertes. Contactez l’institut pour choisir la campagne.")
		case errors.Is(err, references.ErrCampaignNotFound):
			p = problem(404, "CAMPAIGN_NOT_FOUND", "Cette campagne est introuvable.")
		default:
			p = problem(500, "INTERNAL_ERROR", "L’enregistrement est momentanément indisponible. Réessayez.")
			h.logger.Error("draft operation failed")
		}
	}
	if p.Status == 401 {
		http.SetCookie(w, &http.Cookie{Name: CookieName, Path: CookiePath, MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	}
	send(w, p.Status, map[string]any{"error": p})
}
func (h Handler) mutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != h.origin {
		h.failure(w, problem(403, "ORIGIN_FORBIDDEN", "Cette origine n’est pas autorisée."))
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		h.failure(w, problem(415, "JSON_REQUIRED", "Une requête JSON est requise."))
		return false
	}
	return true
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return problem(400, "INVALID_PARAMETERS", "Le contenu de la requête est invalide.")
	}
	if d.Decode(new(any)) != io.EOF {
		return problem(400, "INVALID_PARAMETERS", "Le contenu de la requête est invalide.")
	}
	return nil
}
func session(r *http.Request) ([]byte, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || !ValidSecret(c.Value) {
		return nil, ErrSession
	}
	return HashSecret(c.Value), nil
}
func (h Handler) create(w http.ResponseWriter, r *http.Request) {
	if !h.mutation(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var input struct {
		Campaign string `json:"campaign"`
	}
	if err := decode(w, r, &input); err != nil {
		h.failure(w, err)
		return
	}
	if _, err := r.Cookie(CookieName); err == nil {
		hash, err := session(r)
		if err != nil {
			h.failure(w, err)
			return
		}
		d, err := h.repository.Current(ctx, hash)
		if err != nil {
			h.failure(w, err)
			return
		}
		send(w, 200, d)
		return
	}
	if !h.limiter.allow(r) {
		w.Header().Set("Retry-After", "60")
		h.failure(w, problem(429, "RATE_LIMITED", "Trop de tentatives. Réessayez dans une minute."))
		return
	}
	token, hash, err := NewSecret()
	if err != nil {
		h.failure(w, err)
		return
	}
	d, err := h.repository.Create(ctx, input.Campaign, hash, h.ttl)
	if err != nil {
		h.failure(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: CookiePath, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: int(h.ttl.Seconds()), Expires: d.SessionExpiresAt})
	send(w, 201, d)
}
func (h Handler) current(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	hash, err := session(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	d, err := h.repository.Current(ctx, hash)
	if err != nil {
		h.failure(w, err)
		return
	}
	send(w, 200, d)
}
func (h Handler) update(w http.ResponseWriter, r *http.Request) {
	if !h.mutation(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	hash, err := session(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	kind := strings.TrimPrefix(r.URL.Path, CookiePath+"/current/")
	var input Change
	if err = decode(w, r, &input); err != nil {
		h.failure(w, err)
		return
	}
	if input.Version < 1 || (kind == "profile" && (input.Profile == nil || input.Qualification != "" || input.Program != "" || input.GraduationYear != nil)) || (kind == "academic-background" && (input.Profile != nil || input.Program != "" || input.Qualification == "" || input.GraduationYear == nil)) || (kind == "program" && (input.Profile != nil || input.Qualification != "" || input.GraduationYear != nil || input.Program == "")) {
		h.failure(w, problem(400, "INVALID_PARAMETERS", "Paramètres de modification invalides."))
		return
	}
	d, err := h.repository.Update(ctx, hash, kind, input)
	if err != nil {
		h.failure(w, err)
		return
	}
	send(w, 200, d)
}
