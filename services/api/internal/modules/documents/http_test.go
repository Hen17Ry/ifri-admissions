package documents

import (
	"bytes"
	"ifri-admissions/services/api/internal/modules/drafts"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"testing"
)

func handler(s *Service) http.Handler {
	mux := http.NewServeMux()
	Register(mux, s, "http://localhost:5173", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}
func TestDocumentHTTP(t *testing.T) {
	s, _, _ := fixture()
	secret, _, _ := drafts.NewSecret()
	data, _ := os.ReadFile("testdata/valid.pdf")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="document.pdf"`}, "Content-Type": {"application/pdf"}})
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	req := httptest.NewRequest("POST", base+"/TRANSCRIPT", &body)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Application-Version", "1")
	req.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: secret})
	w := httptest.NewRecorder()
	handler(s).ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	content := httptest.NewRequest("GET", base+"/TRANSCRIPT/content", nil)
	content.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: secret})
	w = httptest.NewRecorder()
	handler(s).ServeHTTP(w, content)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("preview headers: %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("preview body")
	}
}
func TestDocumentsRequireSessionAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		method, path, origin string
		cookie               bool
		status               int
		repoErr              error
	}{
		{"GET", base, "", false, 401, nil}, {"POST", base + "/TRANSCRIPT", "", false, 403, nil}, {"GET", base, "", true, 401, drafts.ErrExpired}, {"GET", "/api/v1/documents/known-uuid", "", true, 404, nil},
	} {
		s, r, _ := fixture()
		r.err = tc.repoErr
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Origin", tc.origin)
		if tc.cookie {
			secret, _, _ := drafts.NewSecret()
			req.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: secret})
		}
		w := httptest.NewRecorder()
		handler(s).ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("expected %d got %d", tc.status, w.Code)
		}
	}
}
