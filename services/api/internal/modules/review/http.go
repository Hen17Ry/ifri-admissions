package review

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"ifri-admissions/services/api/internal/modules/admin"
)

type handler struct {
	rt *admin.Router
	s  *Service
}

// Register mounts the back-office on the admin router: every route declares its permission.
func Register(rt *admin.Router, s *Service) {
	h := handler{rt, s}
	b := admin.BasePath
	rt.Handle("GET", b+"/dashboard", admin.PermDashboard, h.dashboard)
	rt.Handle("GET", b+"/review/reference", admin.PermApplicationsRead, h.reference)
	rt.Handle("GET", b+"/applications", admin.PermApplicationsRead, h.list)
	rt.Handle("GET", b+"/applications/{id}", admin.PermApplicationsRead, h.detail)
	rt.Handle("GET", b+"/applications/{id}/documents/{type}/content", admin.PermApplicationsRead, h.document)
	rt.Handle("GET", b+"/applications/{id}/subjects/{subject}/proof", admin.PermApplicationsRead, h.proofState)
	rt.Handle("POST", b+"/applications/{id}/subjects/{subject}/proof", admin.PermApplicationsRead, h.requestProof)
	rt.Handle("GET", b+"/applications/{id}/subjects/{subject}/proof/image", admin.PermApplicationsRead, h.proofImage)
	for action, perm := range map[string]admin.Permission{"claim": admin.PermReviewClaim, "validate": admin.PermReviewDecide,
		"reject": admin.PermReviewDecide, "reassign": admin.PermReviewReassign, "revert": admin.PermReviewRevert} {
		rt.Handle("POST", b+"/applications/{id}/review/"+action, perm, h.transition(action))
	}
	rt.Handle("POST", b+"/applications/{id}/corrections", admin.PermCorrectionsPropose, h.propose)
	rt.Handle("GET", b+"/corrections", admin.PermCorrectionsDecide, h.queue)
	rt.Handle("POST", b+"/corrections/{id}/approve", admin.PermCorrectionsDecide, h.decide(true))
	rt.Handle("POST", b+"/corrections/{id}/refuse", admin.PermCorrectionsDecide, h.decide(false))
}

func timeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 10*time.Second)
}

func (h handler) dashboard(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	d, err := h.s.Dashboard(ctx, s.Principal, h.rt.Service())
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, d)
}

func (h handler) reference(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ref, err := h.s.Reference(r.Context(), s.Principal)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, ref)
}

func (h handler) list(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	f := Filter{Query: q.Get("q"), Program: q.Get("program"), Qualification: q.Get("qualification"), Status: q.Get("status"), Flag: q.Get("flag"),
		Assignee: q.Get("assignee"), PendingCorrection: q.Get("pendingCorrection") == "true", Sort: q.Get("sort"), Direction: q.Get("direction"), Page: page, PageSize: size}
	ctx, cancel := timeout(r)
	defer cancel()
	list, err := h.s.List(ctx, s.Principal, f)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, list)
}

func (h handler) detail(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	d, err := h.s.Detail(ctx, s.Principal, r.PathValue("id"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, d)
}

// safeFilename keeps a readable, header-safe name ending in .pdf.
func safeFilename(name string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(".-_ ", r) {
			return r
		}
		return '_'
	}, name)
	clean = strings.Trim(clean, " .")
	if len([]rune(clean)) > 100 {
		clean = string([]rune(clean)[:100])
	}
	if clean == "" {
		clean = "document"
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".pdf") {
		clean += ".pdf"
	}
	return clean
}

// document streams a submitted PDF for the in-page preview. This route alone may be framed by
// the administration page itself; every other admin response keeps frame-ancestors 'none'.
func (h handler) document(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	documentType := r.PathValue("type")
	if !map[string]bool{"REQUEST_LETTER": true, "TRANSCRIPT": true, "IDENTITY_DOCUMENT": true, "PARENTAL_COMMITMENT": true}[documentType] {
		h.rt.Fail(w, ErrDocument)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	v, obj, err := h.s.Document(ctx, s.Principal, r.PathValue("id"), documentType)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	defer obj.Body.Close()
	header := w.Header()
	header.Set("Content-Type", "application/pdf")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": safeFilename(v.Filename)}))
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
	header.Set("X-Frame-Options", "SAMEORIGIN")
	http.ServeContent(w, r, "", time.Time{}, obj.Body)
}

func (h handler) proofState(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	p, err := h.s.ProofState(r.Context(), s.Principal, r.PathValue("id"), r.PathValue("subject"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, p)
}

func (h handler) requestProof(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	if err := admin.Decode(w, r, &struct{}{}); err != nil {
		h.rt.Fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	p, err := h.s.RequestProof(ctx, s.Principal, r.PathValue("id"), r.PathValue("subject"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 202, p)
}

func (h handler) proofImage(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	obj, err := h.s.ProofImage(ctx, s.Principal, r.PathValue("id"), r.PathValue("subject"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	defer obj.Body.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.WriteHeader(200)
	_, _ = io.Copy(w, obj.Body)
}

func (h handler) transition(action string) admin.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, s *admin.Session) {
		var input Action
		if err := admin.Decode(w, r, &input); err != nil {
			h.rt.Fail(w, err)
			return
		}
		ctx, cancel := timeout(r)
		defer cancel()
		d, err := h.s.Transition(ctx, s.Principal, r.PathValue("id"), action, input)
		if err != nil {
			h.rt.Fail(w, err)
			return
		}
		h.rt.Send(w, 200, d)
	}
}

func (h handler) propose(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	var input Proposal
	if err := admin.Decode(w, r, &input); err != nil {
		h.rt.Fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	d, err := h.s.Propose(ctx, s.Principal, r.PathValue("id"), input)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 201, d)
}

func (h handler) queue(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	list, err := h.s.PendingCorrections(r.Context(), s.Principal)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, map[string]any{"corrections": list})
}

func (h handler) decide(approve bool) admin.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, s *admin.Session) {
		var input Decision
		if err := admin.Decode(w, r, &input); err != nil {
			h.rt.Fail(w, err)
			return
		}
		ctx, cancel := timeout(r)
		defer cancel()
		d, err := h.s.Decide(ctx, s.Principal, r.PathValue("id"), approve, input)
		if err != nil {
			h.rt.Fail(w, err)
			return
		}
		h.rt.Send(w, 200, d)
	}
}
