package documents

import (
	"context"
	"encoding/json"
	"errors"
	"ifri-admissions/services/api/internal/modules/drafts"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const base = drafts.CookiePath + "/current/documents"

type Handler struct {
	s      *Service
	origin string
	secure bool
	logger *slog.Logger
}

func Register(mux *http.ServeMux, s *Service, origin string, secure bool, logger *slog.Logger) {
	h := Handler{s, origin, secure, logger}
	mux.HandleFunc("GET "+base, h.list)
	mux.HandleFunc("POST "+base+"/{documentType}", h.upload)
	mux.HandleFunc("GET "+base+"/{documentType}/content", h.content)
	mux.HandleFunc("GET "+base+"/{documentType}/download", h.content)
}
func (h Handler) fail(w http.ResponseWriter, err error) {
	var p *drafts.Problem
	if !errors.As(err, &p) {
		p = &drafts.Problem{Status: 500, Code: "DOCUMENT_OPERATION_FAILED", Message: "Le document n’a pas pu être enregistré. Rechargez la liste avant de réessayer."}
		h.logger.Error("document operation failed")
	}
	if p.Status == 401 {
		http.SetCookie(w, &http.Cookie{Name: drafts.CookieName, Path: drafts.CookiePath, MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": p})
}
func hash(r *http.Request) ([]byte, error) {
	c, err := r.Cookie(drafts.CookieName)
	if err != nil || !drafts.ValidSecret(c.Value) {
		return nil, drafts.ErrSession
	}
	return drafts.HashSecret(c.Value), nil
}
func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	key, err := hash(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	out, err := h.s.List(ctx, key)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
func (h Handler) upload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != h.origin {
		h.fail(w, issue(403, "ORIGIN_FORBIDDEN", "Cette origine n’est pas autorisée."))
		return
	}
	key, err := hash(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	code := r.PathValue("documentType")
	if !Known(code) {
		h.fail(w, issue(400, "DOCUMENT_TYPE_INVALID", "Ce type de document n’est pas reconnu."))
		return
	}
	version, err := strconv.ParseInt(r.Header.Get("X-Application-Version"), 10, 64)
	if err != nil || version < 1 {
		h.fail(w, issue(400, "APPLICATION_VERSION_REQUIRED", "Rechargez le brouillon avant l’envoi."))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.s.max+65536)
	reader, err := r.MultipartReader()
	if err != nil {
		h.fail(w, issue(415, "MULTIPART_REQUIRED", "Choisissez un fichier PDF à envoyer."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if _, err = h.s.authorize(ctx, key, version, code); err != nil {
		h.fail(w, err)
		return
	}
	select {
	case h.s.slots <- struct{}{}:
		defer func() { <-h.s.slots }()
	case <-ctx.Done():
		h.fail(w, issue(408, "UPLOAD_INTERRUPTED", "L’envoi a été interrompu."))
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		h.fail(w, issue(400, "FILE_REQUIRED", "Choisissez un fichier PDF."))
		return
	}
	defer part.Close()
	// Ensure no extra fields/files before any external write: first spool/validate, then upload that file.
	upload, err := Validate(ctx, part, part.FileName(), part.Header.Get("Content-Type"), h.s.max, h.s.inspector)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer upload.Close()
	if _, err = reader.NextPart(); err != io.EOF {
		h.fail(w, issue(400, "SINGLE_FILE_REQUIRED", "Envoyez un seul document à la fois."))
		return
	}
	// Only the fully validated, single-file temporary object reaches external storage.
	out, err := h.s.uploadValidated(ctx, key, version, code, upload)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(out)
}
func (h Handler) content(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	key, err := hash(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	v, obj, err := h.s.Content(ctx, key, r.PathValue("documentType"))
	if err != nil {
		h.fail(w, err)
		return
	}
	defer obj.Body.Close()
	disposition := "inline"
	if strings.HasSuffix(r.URL.Path, "/download") {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": v.Filename}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", "\""+v.ID+"\"")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeContent(w, r, v.Filename, v.CreatedAt, obj.Body)
}
