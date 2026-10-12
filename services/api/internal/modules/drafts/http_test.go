package drafts

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
)

type fakeRepository struct {
	err     error
	hash    []byte
	changes int
}

func (f *fakeRepository) Create(_ context.Context, _ string, hash []byte, _ time.Duration) (Draft, error) {
	f.hash = hash
	return Draft{ID: "test", State: "DRAFT", Version: 1, SessionExpiresAt: time.Now().Add(time.Hour)}, f.err
}
func (f *fakeRepository) Current(context.Context, []byte) (Draft, error) {
	return Draft{ID: "test", State: "DRAFT", Version: 1}, f.err
}
func (f *fakeRepository) Update(context.Context, []byte, string, Change) (Draft, error) {
	f.changes++
	return Draft{ID: "test", State: "DRAFT", Version: 2}, f.err
}
func testHandler(repo Repository, secure bool) http.Handler {
	mux := http.NewServeMux()
	Register(mux, repo, time.Hour, "http://localhost:5173", secure, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}
func request(method, path, body, token, origin string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	}
	return r
}
func TestDraftHTTPAndCookies(t *testing.T) {
	for _, secure := range []bool{false, true} {
		repo := &fakeRepository{}
		w := httptest.NewRecorder()
		testHandler(repo, secure).ServeHTTP(w, request("POST", CookiePath, `{}`, "", "http://localhost:5173"))
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing session cookie")
		}
		c := cookies[0]
		if !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode || c.Path != CookiePath || c.MaxAge != 3600 || !ValidSecret(c.Value) {
			t.Fatal("invalid cookie configuration")
		}
		if string(repo.hash) == c.Value || string(repo.hash) != string(HashSecret(c.Value)) {
			t.Fatal("raw secret stored")
		}
		if strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), "secret_hash") {
			t.Fatal("session exposed in DTO")
		}
	}
}
func TestDraftHTTPFailures(t *testing.T) {
	token, _, _ := NewSecret()
	for _, tc := range []struct {
		name, method, path, body, token, origin string
		repoError                               error
		status                                  int
		code                                    string
	}{
		{"no session", "GET", CookiePath + "/current", "", "", "", nil, 401, "DRAFT_SESSION_REQUIRED"},
		{"bad secret", "GET", CookiePath + "/current", "", "uuid", "", nil, 401, "DRAFT_SESSION_REQUIRED"},
		{"expired", "GET", CookiePath + "/current", "", token, "", ErrExpired, 401, "DRAFT_SESSION_EXPIRED"},
		{"revoked", "GET", CookiePath + "/current", "", token, "", ErrRevoked, 401, "DRAFT_SESSION_REVOKED"},
		{"missing Origin", "POST", CookiePath, `{}`, "", "", nil, 403, "ORIGIN_FORBIDDEN"},
		{"wrong Origin", "PATCH", CookiePath + "/current/profile", `{}`, token, "https://evil.example", nil, 403, "ORIGIN_FORBIDDEN"},
		{"unknown fields", "POST", CookiePath, `{"state":"SUBMITTED"}`, "", "http://localhost:5173", nil, 400, "INVALID_PARAMETERS"},
		{"version required", "PATCH", CookiePath + "/current/program", `{"program":"GL"}`, token, "http://localhost:5173", nil, 400, "INVALID_PARAMETERS"},
		{"ineligible", "PATCH", CookiePath + "/current/program", `{"version":1,"program":"GL"}`, token, "http://localhost:5173", ErrIneligible, 422, "APPLICATION_NOT_ELIGIBLE"},
		{"safe internal error", "GET", CookiePath + "/current", "", token, "", errors.New("SECRET SQL"), 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{err: tc.repoError}
			w := httptest.NewRecorder()
			testHandler(repo, false).ServeHTTP(w, request(tc.method, tc.path, tc.body, tc.token, tc.origin))
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var body struct {
				Error Problem `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != tc.code {
				t.Fatal(w.Body)
			}
			if strings.Contains(w.Body.String(), "SECRET SQL") {
				t.Fatal("internal error leaked")
			}
		})
	}
}
func TestProfileNormalization(t *testing.T) {
	p, err := NormalizeProfile(Profile{"  Dossou  ", " Élise ", " TEST@Example.COM ", "00229 01-23-45-67-89"})
	if err != nil || p.LastName != "Dossou" || p.FirstNames != "Élise" || p.Email != "test@example.com" || p.Phone != "+2290123456789" {
		t.Fatalf("normalization: %+v %v", p, err)
	}
	for _, p := range []Profile{{Email: "Display <x@example.com>"}, {Email: "a@localhost"}, {Phone: "123456"}, {LastName: "Bad\nname"}, {Email: "x@example.com\n" + "Header"}} {
		if _, err := NormalizeProfile(p); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
	for _, phone := range []string{"+33123456789", "+12025550123", "+447700900123"} {
		if _, err := NormalizeProfile(Profile{Phone: phone}); err != nil {
			t.Fatal("international number rejected")
		}
	}
}
func TestNoUUIDAccessAndNoSubmission(t *testing.T) {
	h := testHandler(&fakeRepository{}, false)
	for _, path := range []string{"/api/v1/applications/123", "/api/v1/applications/drafts/123", "/api/v1/applications/drafts/current/submit"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("unexpected public route", path)
		}
	}
}

func TestCreateRateLimit(t *testing.T) {
	h := testHandler(&fakeRepository{}, false)
	for i := 0; i < 6; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request("POST", CookiePath, `{}`, "", "http://localhost:5173"))
		want := 201
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
}
