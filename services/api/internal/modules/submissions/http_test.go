package submissions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ifri-admissions/services/api/internal/modules/drafts"
)

type fakeService struct {
	err     error
	calls   int
	version int64
	receipt Receipt
}

func (f *fakeService) Submit(_ context.Context, _ []byte, version int64) (Receipt, Outcome, error) {
	f.calls++
	f.version = version
	return f.receipt, Outcome{ApplicationID: "app", SubmissionID: "sub", Replay: f.calls > 1}, f.err
}
func (f *fakeService) Receipt(context.Context, []byte) (Receipt, error) { return f.receipt, f.err }

const origin = "http://localhost:5173"

func call(service Service, method, body, token, from, contentType string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	Register(mux, service, origin, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(method, drafts.CookiePath+"/current/submission", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if from != "" {
		r.Header.Set("Origin", from)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: token})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func code(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error drafts.Problem `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error.Code
}

func TestSubmissionHTTPReplaysTheSameReceipt(t *testing.T) {
	token, _, _ := drafts.NewSecret()
	service := &fakeService{receipt: Receipt{ApplicationNumber: "IFRI-2026-GL-000001", SubmittedAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), SessionExpiresAt: time.Now().Add(30 * 24 * time.Hour)}}
	first := call(service, "POST", `{"version":7}`, token, origin, "application/json")
	second := call(service, "POST", `{"version":7}`, token, origin, "application/json")
	if first.Code != 200 || second.Code != 200 || first.Body.String() != second.Body.String() || service.version != 7 {
		t.Fatal(first.Code, second.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), "IFRI-2026-GL-000001") || strings.Contains(strings.ToLower(first.Body.String()), "average") {
		t.Fatal("receipt must carry the number and never the average", first.Body.String())
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != token || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Path != drafts.CookiePath || cookies[0].MaxAge < 29*24*3600 {
		t.Fatal("read-only session not extended", cookies)
	}
}

func TestSubmissionHTTPFailures(t *testing.T) {
	token, _, _ := drafts.NewSecret()
	for _, tc := range []struct {
		name, method, body, token, origin, contentType string
		err                                            error
		status                                         int
		code                                           string
	}{
		{"origin required", "POST", `{"version":1}`, token, "", "application/json", nil, 403, "ORIGIN_FORBIDDEN"},
		{"foreign origin", "POST", `{"version":1}`, token, "https://evil.example", "application/json", nil, 403, "ORIGIN_FORBIDDEN"},
		{"json required", "POST", `{"version":1}`, token, origin, "text/plain", nil, 415, "JSON_REQUIRED"},
		{"no session", "POST", `{"version":1}`, "", origin, "application/json", nil, 401, "DRAFT_SESSION_REQUIRED"},
		{"version required", "POST", `{"version":0}`, token, origin, "application/json", nil, 400, "INVALID_PARAMETERS"},
		{"unknown field", "POST", `{"version":1,"average":"20"}`, token, origin, "application/json", nil, 400, "INVALID_PARAMETERS"},
		{"trailing data", "POST", `{"version":1}{}`, token, origin, "application/json", nil, 400, "INVALID_PARAMETERS"},
		{"locked", "POST", `{"version":1}`, token, origin, "application/json", drafts.ErrLocked, 409, "APPLICATION_LOCKED"},
		{"stale version", "POST", `{"version":1}`, token, origin, "application/json", ErrVersionConflict, 409, "VERSION_CONFLICT"},
		{"closed", "POST", `{"version":1}`, token, origin, "application/json", ErrCampaignClosed, 409, "CAMPAIGN_CLOSED"},
		{"rules changed", "POST", `{"version":1}`, token, origin, "application/json", ErrRulesChanged, 409, "ADMISSION_RULES_CHANGED"},
		{"expired", "POST", `{"version":1}`, token, origin, "application/json", drafts.ErrExpired, 401, "DRAFT_SESSION_EXPIRED"},
		{"internal error hidden", "POST", `{"version":1}`, token, origin, "application/json", errors.New("pq: secret detail"), 500, "SUBMISSION_FAILED"},
		{"receipt without session", "GET", "", "", "", "", nil, 401, "DRAFT_SESSION_REQUIRED"},
		{"receipt of a draft", "GET", "", token, "", "", ErrNotSubmitted, 404, "SUBMISSION_NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(&fakeService{err: tc.err}, tc.method, tc.body, tc.token, tc.origin, tc.contentType)
			if w.Code != tc.status || code(t, w) != tc.code || strings.Contains(w.Body.String(), "secret detail") {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status == 401 && !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
				t.Fatal("ended session cookie kept")
			}
		})
	}
}

func TestReceiptHTTP(t *testing.T) {
	token, _, _ := drafts.NewSecret()
	w := call(&fakeService{receipt: Receipt{ApplicationNumber: "IFRI-2026-GL-000002"}}, "GET", "", token, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "IFRI-2026-GL-000002") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
}
