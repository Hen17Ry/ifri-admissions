package selection

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"ifri-admissions/services/api/internal/modules/admin"
)

type handler struct {
	rt *admin.Router
	s  *Service
}

// Register mounts ranking and selection on the admin router: every route declares its permission.
func Register(rt *admin.Router, s *Service) {
	h := handler{rt, s}
	b := admin.BasePath + "/selection"
	rt.Handle("GET", b+"/offers", admin.PermRankingRead, h.offers)
	rt.Handle("GET", b+"/offers/{id}", admin.PermRankingRead, h.view)
	rt.Handle("PUT", b+"/offers/{id}/settings", admin.PermRankingConfigure, h.settings)
	rt.Handle("POST", b+"/offers/{id}/freeze", admin.PermSelectionValidate, h.freeze)
	rt.Handle("POST", b+"/offers/{id}/withdrawals", admin.PermSelectionManage, h.move("WITHDRAWN"))
	rt.Handle("POST", b+"/offers/{id}/promotions", admin.PermSelectionManage, h.move("PROMOTED"))
	rt.Handle("GET", b+"/offers/{id}/export.csv", admin.PermSelectionExport, h.csv)
	rt.Handle("POST", b+"/offers/{id}/exports", admin.PermSelectionExport, h.requestPDF)
	rt.Handle("GET", b+"/exports/{id}", admin.PermSelectionExport, h.exportState)
	rt.Handle("GET", b+"/exports/{id}/content", admin.PermSelectionExport, h.exportContent)
}

func timeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

func (h handler) offers(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	list, err := h.s.Offers(r.Context(), s.Principal)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, map[string]any{"offers": list})
}

func (h handler) view(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	v, err := h.s.View(ctx, s.Principal, r.PathValue("id"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, v)
}

func (h handler) settings(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	var input Settings
	if err := admin.Decode(w, r, &input); err != nil {
		h.rt.Fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	v, err := h.s.SetSettings(ctx, s.Principal, r.PathValue("id"), input)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, v)
}

func (h handler) freeze(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	var input Settings
	if err := admin.Decode(w, r, &input); err != nil {
		h.rt.Fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	v, err := h.s.Freeze(ctx, s.Principal, r.PathValue("id"), input)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, v)
}

func (h handler) move(kind string) admin.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, s *admin.Session) {
		var input Move
		if err := admin.Decode(w, r, &input); err != nil {
			h.rt.Fail(w, err)
			return
		}
		ctx, cancel := timeout(r)
		defer cancel()
		v, err := h.s.Record(ctx, s.Principal, r.PathValue("id"), kind, input)
		if err != nil {
			h.rt.Fail(w, err)
			return
		}
		h.rt.Send(w, 200, v)
	}
}

func attachment(w http.ResponseWriter, contentType, filename string, size int) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Length", strconv.Itoa(size))
}

func (h handler) csv(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	d, body, err := h.s.CSV(ctx, s.Principal, r.PathValue("id"), r.URL.Query().Get("variant"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	attachment(w, "text/csv; charset=utf-8", d.Filename+".csv", len(body))
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func (h handler) requestPDF(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	var input struct {
		Variant string `json:"variant"`
	}
	if err := admin.Decode(w, r, &input); err != nil {
		h.rt.Fail(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	x, err := h.s.RequestPDF(ctx, s.Principal, r.PathValue("id"), input.Variant)
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 202, x)
}

func (h handler) exportState(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	x, err := h.s.ExportState(r.Context(), s.Principal, r.PathValue("id"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	h.rt.Send(w, 200, x)
}

func (h handler) exportContent(w http.ResponseWriter, r *http.Request, s *admin.Session) {
	ctx, cancel := timeout(r)
	defer cancel()
	obj, filename, err := h.s.ExportContent(ctx, s.Principal, r.PathValue("id"))
	if err != nil {
		h.rt.Fail(w, err)
		return
	}
	defer obj.Body.Close()
	attachment(w, "application/pdf", filename, int(obj.Size))
	w.WriteHeader(200)
	_, _ = io.Copy(w, obj.Body)
}
