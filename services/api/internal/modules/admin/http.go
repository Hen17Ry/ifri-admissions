package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HandlerFunc receives the session resolved by the guard (nil on public routes).
type HandlerFunc func(http.ResponseWriter, *http.Request, *Session)

// Route is one registered admin route and the permission it declares.
type Route struct {
	Method     string
	Path       string
	Permission Permission
}

// Router is the only way to add an admin route: a route without permission cannot be built.
type Router struct {
	mux    *http.ServeMux
	routes []Route
	h      *Handler
}

func (rt *Router) Handle(method, path string, perm Permission, fn HandlerFunc) {
	if perm == "" || !strings.HasPrefix(path, BasePath+"/") {
		panic("admin route " + method + " " + path + " must declare a permission under " + BasePath)
	}
	route := Route{Method: method, Path: path, Permission: perm}
	rt.routes = append(rt.routes, route)
	rt.mux.Handle(method+" "+path, rt.h.guard(route, fn))
}

func (rt *Router) Routes() []Route { return append([]Route(nil), rt.routes...) }

type Handler struct {
	service *Service
	origin  string
	secure  bool
	logger  *slog.Logger
}

// Register mounts the admin API under /api/v1/admin/ behind strict security headers.
func Register(mux *http.ServeMux, service *Service, origin string, secure bool, logger *slog.Logger) *Router {
	h := &Handler{service: service, origin: origin, secure: secure, logger: logger}
	rt := &Router{mux: http.NewServeMux(), h: h}
	b := BasePath
	rt.Handle("GET", b+"/session", PermAnySession, h.session)
	rt.Handle("POST", b+"/auth/login", PermPublic, h.login)
	rt.Handle("POST", b+"/auth/logout", PermAnySession, h.logout)
	rt.Handle("POST", b+"/auth/mfa/enrollment", PermEnrollment, h.startEnrollment)
	rt.Handle("POST", b+"/auth/mfa/enrollment/confirm", PermEnrollment, h.confirmEnrollment)
	rt.Handle("POST", b+"/auth/mfa/verify", PermVerification, h.verify)
	rt.Handle("POST", b+"/auth/password", PermSelf, h.changePassword)
	rt.Handle("POST", b+"/auth/recovery-codes", PermSelf, h.recoveryCodes)
	rt.Handle("POST", b+"/activation/inspect", PermPublic, h.inspectActivation)
	rt.Handle("POST", b+"/activation", PermPublic, h.activate)
	rt.Handle("GET", b+"/accounts", PermAccountsRead, h.accounts)
	rt.Handle("POST", b+"/accounts", PermAccountsManage, h.createAccount)
	rt.Handle("POST", b+"/accounts/{id}/activation-link", PermAccountsManage, h.reissueActivation)
	rt.Handle("POST", b+"/accounts/{id}/disable", PermAccountsManage, h.status(false))
	rt.Handle("POST", b+"/accounts/{id}/enable", PermAccountsManage, h.status(true))
	rt.Handle("PUT", b+"/accounts/{id}/grants", PermAccountsManage, h.grants)
	rt.Handle("POST", b+"/accounts/{id}/mfa/reset", PermAccountsManage, h.resetMFA)
	rt.Handle("GET", b+"/programs", PermAccountsRead, h.programs)
	rt.Handle("GET", b+"/audit-events", PermAuditRead, h.audit)
	mux.Handle(b+"/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SecurityHeaders(w.Header(), secure)
		if _, pattern := rt.mux.Handler(r); pattern == "" {
			h.fail(w, problem(404, "NOT_FOUND", "Cette ressource n’existe pas."))
			return
		}
		rt.mux.ServeHTTP(w, r)
	}))
	return rt
}

// SecurityHeaders: API answers are data only, never framed, cached or referred.
func SecurityHeaders(header http.Header, secure bool) {
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	if secure {
		header.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	}
}

func (h *Handler) send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var p *Problem
	if !errors.As(err, &p) {
		p = &Problem{Status: 500, Code: "ADMIN_OPERATION_FAILED", Message: "L’opération n’a pas pu aboutir. Réessayez."}
		h.logger.Error("admin operation failed")
	}
	if p.Status == 401 {
		h.clearCookie(w)
	}
	if p.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(p.RetryAfter.Round(time.Second).Seconds())+1))
	}
	h.send(w, p.Status, map[string]any{"error": p})
}

func (h *Handler) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: BasePath, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode})
}

func (h *Handler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Path: BasePath, MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteStrictMode})
}

func cookie(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// clientIP uses the socket address only; a forwarded header is never trusted here.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// guard enforces, in order: Origin + JSON on mutations, session, stage, CSRF, permission.
func (h *Handler) guard(route Route, fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutation {
			if r.Header.Get("Origin") != h.origin {
				h.fail(w, ErrOrigin)
				return
			}
			if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || media != "application/json" {
				h.fail(w, ErrJSON)
				return
			}
		}
		if route.Permission == PermPublic {
			fn(w, r, nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		session, err := h.service.Authenticate(ctx, cookie(r))
		if err != nil {
			h.fail(w, err)
			return
		}
		switch route.Permission {
		case PermAnySession:
		case PermEnrollment, PermVerification:
			if (route.Permission == PermEnrollment) != (session.Stage == StageEnrollment) || (route.Permission == PermVerification) != (session.Stage == StageVerification) {
				h.fail(w, ErrStage)
				return
			}
		default:
			if session.Stage != StageActive {
				h.fail(w, ErrStage)
				return
			}
		}
		if mutation && !h.service.Keys.ValidCSRF(session.TokenHash, r.Header.Get(CSRFHeader)) {
			h.fail(w, ErrCSRF)
			return
		}
		switch route.Permission {
		case PermAnySession, PermEnrollment, PermVerification, PermSelf:
		default:
			if !session.Principal.Can(route.Permission) {
				h.fail(w, ErrForbidden)
				return
			}
		}
		fn(w, r, session)
	})
}

func decode(w http.ResponseWriter, r *http.Request, value any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func timeout(r *http.Request) (context.Context, context.CancelFunc) {
	// argon2 waits for a free slot: a generous bound, still finite.
	return context.WithTimeout(r.Context(), 15*time.Second)
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request, s *Session) {
	h.send(w, 200, h.service.View(s))
}

type stageResponse struct {
	Stage         string   `json:"stage"`
	CSRFToken     string   `json:"csrfToken"`
	RecoveryCodes []string `json:"recoveryCodes,omitempty"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request, _ *Session) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &input); err != nil || len(input.Email) > 254 {
		h.fail(w, ErrInvalid)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	token, stage, err := h.service.Login(ctx, input.Email, input.Password, clientIP(r), cookie(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, token)
	h.send(w, 200, stageResponse{Stage: stage, CSRFToken: h.service.Keys.CSRF(hashToken(token))})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request, s *Session) {
	if err := h.service.Logout(r.Context(), s); err != nil {
		h.fail(w, err)
		return
	}
	h.clearCookie(w)
	h.send(w, 200, map[string]bool{"loggedOut": true})
}

// startEnrollment returns the secret and its QR code once, during enrolment only; the response
// is no-store and never logged.
func (h *Handler) startEnrollment(w http.ResponseWriter, r *http.Request, s *Session) {
	enrollment, err := h.service.StartEnrollment(r.Context(), s)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, enrollment)
}

func (h *Handler) code(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &input); err != nil || len(input.Code) > 32 {
		h.fail(w, ErrInvalid)
		return "", false
	}
	return input.Code, true
}

func (h *Handler) confirmEnrollment(w http.ResponseWriter, r *http.Request, s *Session) {
	code, ok := h.code(w, r)
	if !ok {
		return
	}
	token, codes, err := h.service.ConfirmEnrollment(r.Context(), s, code, clientIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, token)
	h.send(w, 200, stageResponse{Stage: StageActive, CSRFToken: h.service.Keys.CSRF(hashToken(token)), RecoveryCodes: codes})
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request, s *Session) {
	code, ok := h.code(w, r)
	if !ok {
		return
	}
	token, err := h.service.VerifyMFA(r.Context(), s, code, clientIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, token)
	h.send(w, 200, stageResponse{Stage: StageActive, CSRFToken: h.service.Keys.CSRF(hashToken(token))})
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request, s *Session) {
	var input struct {
		Current string `json:"currentPassword"`
		Next    string `json:"newPassword"`
	}
	if err := decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	if err := h.service.ChangePassword(ctx, s, input.Current, input.Next); err != nil {
		h.fail(w, err)
		return
	}
	// Every session is revoked, this one included: sign in again with the new password.
	h.clearCookie(w)
	h.send(w, 200, map[string]bool{"reauthenticate": true})
}

func (h *Handler) recoveryCodes(w http.ResponseWriter, r *http.Request, s *Session) {
	codes, err := h.service.RegenerateRecoveryCodes(r.Context(), s)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, map[string][]string{"recoveryCodes": codes})
}

func (h *Handler) inspectActivation(w http.ResponseWriter, r *http.Request, _ *Session) {
	var input struct {
		Token string `json:"token"`
	}
	if err := decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	email, name, err := h.service.InspectActivation(r.Context(), input.Token, clientIP(r))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, map[string]string{"email": email, "displayName": name})
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request, _ *Session) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	if err := h.service.Activate(ctx, input.Token, input.Password, clientIP(r)); err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, map[string]bool{"activated": true})
}

func (h *Handler) accounts(w http.ResponseWriter, r *http.Request, _ *Session) {
	accounts, err := h.service.Accounts(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, map[string]any{"accounts": accounts})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request, s *Session) {
	var input AccountInput
	if err := decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	activation, err := h.service.CreateAccount(r.Context(), s.Principal, input)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 201, activation)
}

func (h *Handler) reissueActivation(w http.ResponseWriter, r *http.Request, s *Session) {
	if err := decode(w, r, &struct{}{}); err != nil {
		h.fail(w, err)
		return
	}
	activation, err := h.service.ReissueActivation(r.Context(), s.Principal, r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, activation)
}

func (h *Handler) status(enabled bool) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, s *Session) {
		if err := decode(w, r, &struct{}{}); err != nil {
			h.fail(w, err)
			return
		}
		account, err := h.service.SetStatus(r.Context(), adminActor(s.Principal.UserID), r.PathValue("id"), enabled)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.send(w, 200, account)
	}
}

func (h *Handler) grants(w http.ResponseWriter, r *http.Request, s *Session) {
	var input struct {
		Grants []GrantInput `json:"grants"`
	}
	if err := decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	account, err := h.service.SetGrants(r.Context(), s.Principal, r.PathValue("id"), input.Grants)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, account)
}

func (h *Handler) resetMFA(w http.ResponseWriter, r *http.Request, s *Session) {
	if err := decode(w, r, &struct{}{}); err != nil {
		h.fail(w, err)
		return
	}
	account, err := h.service.ResetMFA(r.Context(), adminActor(s.Principal.UserID), r.PathValue("id"), map[string]any{"via": "ADMIN"})
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, account)
}

func (h *Handler) programs(w http.ResponseWriter, r *http.Request, _ *Session) {
	programs, err := h.service.Programs(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, map[string]any{"programs": programs})
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request, _ *Session) {
	q := r.URL.Query()
	filter := AuditFilter{EventType: q.Get("eventType"), ActorType: q.Get("actorType"), TargetID: q.Get("targetId"), Cursor: q.Get("cursor")}
	for key, target := range map[string]**time.Time{"from": &filter.From, "to": &filter.To} {
		if value := q.Get(key); value != "" {
			t, err := time.Parse(time.RFC3339, value)
			if err != nil {
				h.fail(w, ErrInvalid)
				return
			}
			*target = &t
		}
	}
	page, err := h.service.Audit(r.Context(), filter)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.send(w, 200, page)
}
