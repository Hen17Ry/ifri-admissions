package analysis

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalAuthorization(t *testing.T) {
	h := Handler{Secret: strings.Repeat("s", 40)}
	m := http.NewServeMux()
	h.RegisterInternal(m)
	for _, route := range []string{"/internal/ocr/ready", "/internal/ocr/123/claim", "/internal/ocr/123/result", "/internal/ocr/123/document"} {
		method := "POST"
		if strings.HasSuffix(route, "ready") || strings.HasSuffix(route, "document") {
			method = "GET"
		}
		r := httptest.NewRequest(method, route, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s = %d", route, w.Code)
		}
	}
	public := http.NewServeMux()
	h.RegisterPublic(public)
	w := httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("POST", "/internal/ocr/123/result", nil))
	if w.Code != 404 {
		t.Fatal("internal callback exposed on public mux")
	}
}
func TestCandidateNoSessionOrOrigin(t *testing.T) {
	h := Handler{Origin: "http://localhost:5173"}
	m := http.NewServeMux()
	h.RegisterPublic(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/applications/drafts/current/transcript-analysis", nil))
	if w.Code != 401 {
		t.Fatal("analysis without session")
	}
	for _, action := range []string{"confirm", "reject"} {
		w = httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/applications/drafts/current/transcript-analysis/"+action, strings.NewReader(`{}`)))
		if w.Code != 403 {
			t.Fatal("mutation without Origin")
		}
	}
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("PATCH", "/api/v1/applications/drafts/current/transcript-analysis/grade", strings.NewReader(`{"grade":20}`)))
	if w.Code != 404 {
		t.Fatal("manual grade endpoint")
	}
}
