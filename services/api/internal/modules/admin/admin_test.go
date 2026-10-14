package admin

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
)

// countingHasher records every argon2 call; it must stay at zero for refused requests.
type countingHasher struct{ calls atomic.Int32 }

func (h *countingHasher) Hash(context.Context, string) (string, error) {
	h.calls.Add(1)
	return "$argon2id$v=19$m=1,t=1,p=1$AA$AA", nil
}
func (h *countingHasher) Verify(context.Context, string, string) (bool, error) {
	h.calls.Add(1)
	return false, nil
}
func (h *countingHasher) VerifyDummy(context.Context, string) error { h.calls.Add(1); return nil }
func (h *countingHasher) NeedsRehash(string) bool                   { return false }

// blockingLimiter refuses one policy (or all with "*") and records the keys it saw.
type blockingLimiter struct {
	block string
	keys  []string
}

func (l *blockingLimiter) Attempt(_ context.Context, p ratelimit.Policy, key string) (time.Duration, error) {
	l.keys = append(l.keys, p.Name+":"+key)
	if l.block == "*" || l.block == p.Name {
		return time.Minute, nil
	}
	return 0, nil
}
func (l *blockingLimiter) Failure(context.Context, ratelimit.Policy, string) (time.Duration, error) {
	return 0, nil
}
func (l *blockingLimiter) Reset(context.Context, ratelimit.Policy, string) error { return nil }

func testService(t *testing.T, limiter ratelimit.Limiter, hasher Hasher) *Service {
	t.Helper()
	keys, err := secrets.Derive(bytes.Repeat([]byte{5}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Pool is nil on purpose: a refused request must not reach the database either.
	return &Service{Keys: keys, Hasher: hasher, Limiter: limiter, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), MFARequired: true,
		Idle: 30 * time.Minute, Absolute: 8 * time.Hour, ActivationTTL: 72 * time.Hour, Origin: "http://localhost:5173"}
}

func TestBlockedRequestsNeverRunArgon2(t *testing.T) {
	for _, policy := range []string{PolicyLoginIP.Name, PolicyLoginAccount.Name} {
		hasher := &countingHasher{}
		limiter := &blockingLimiter{block: policy}
		s := testService(t, limiter, hasher)
		// Invented emails: each would otherwise cost a 64 MiB dummy argon2.
		for i := range 5 {
			_, _, err := s.Login(context.Background(), "invented"+string(rune('a'+i))+"@example.invalid", "any password at all", "192.0.2.10", "")
			if p, ok := err.(*Problem); !ok || p.Status != 429 || p.RetryAfter <= 0 {
				t.Fatalf("%s: %v", policy, err)
			}
		}
		if hasher.calls.Load() != 0 {
			t.Fatalf("%s: %d argon2 computations for blocked requests", policy, hasher.calls.Load())
		}
		for _, key := range limiter.keys {
			if strings.Contains(key, "invented") || strings.Contains(key, "192.0.2.10") {
				t.Fatal("clear identifier or IP in a rate-limit key", key)
			}
		}
	}
	hasher := &countingHasher{}
	s := testService(t, &blockingLimiter{block: PolicyActivationIP.Name}, hasher)
	if err := s.Activate(context.Background(), strings.Repeat("A", 43), "a long enough passphrase", "192.0.2.10"); err == nil || err.(*Problem).Status != 429 {
		t.Fatal("activation not limited", err)
	}
	s = testService(t, &blockingLimiter{block: PolicyPassword.Name}, hasher)
	if err := s.ChangePassword(context.Background(), &Session{Principal: Principal{UserID: "u", Email: "a@b.c"}}, "old", "a long enough passphrase"); err == nil || err.(*Problem).Status != 429 {
		t.Fatal("password change not limited", err)
	}
	s = testService(t, &blockingLimiter{block: PolicyMFA.Name}, hasher)
	if _, err := s.VerifyMFA(context.Background(), &Session{Principal: Principal{UserID: "u"}}, "123456", "192.0.2.10"); err == nil || err.(*Problem).Status != 429 {
		t.Fatal("MFA not limited", err)
	}
	if hasher.calls.Load() != 0 {
		t.Fatal("argon2 ran for a blocked request")
	}
}

func TestRoutesAllDeclareAPermission(t *testing.T) {
	mux := http.NewServeMux()
	s := testService(t, &blockingLimiter{}, &countingHasher{})
	router := Register(mux, s, "http://localhost:5173", false, s.Logger)
	routes := router.Routes()
	if len(routes) < 19 {
		t.Fatal("routes missing", len(routes))
	}
	markers := []Permission{PermPublic, PermAnySession, PermEnrollment, PermVerification, PermSelf}
	for _, r := range routes {
		known := slices.Contains(markers, r.Permission)
		for _, perms := range RolePermissions {
			known = known || slices.Contains(perms, r.Permission)
		}
		if r.Permission == "" || !known || !strings.HasPrefix(r.Path, BasePath+"/") {
			t.Fatalf("%s %s declares %q", r.Method, r.Path, r.Permission)
		}
		if r.Permission == PermPublic && !slices.Contains([]string{BasePath + "/auth/login", BasePath + "/activation", BasePath + "/activation/inspect"}, r.Path) {
			t.Fatal("unexpected public admin route", r.Path)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a route without permission was accepted")
		}
	}()
	router.Handle("GET", BasePath+"/undeclared", "", func(http.ResponseWriter, *http.Request, *Session) {})
}

func TestUnknownAdminPathAndHeaders(t *testing.T) {
	mux := http.NewServeMux()
	s := testService(t, &blockingLimiter{}, &countingHasher{})
	Register(mux, s, "http://localhost:5173", true, s.Logger)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", BasePath+"/secret-debug", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	for header, want := range map[string]string{"Cache-Control": "no-store", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'", "Strict-Transport-Security": "max-age=63072000; includeSubDomains"} {
		if w.Header().Get(header) != want {
			t.Fatalf("%s: %q", header, w.Header().Get(header))
		}
	}
	// Public mutations still require the exact Origin and JSON, before any credential work.
	for _, tc := range []struct {
		origin, contentType string
		status              int
	}{{"", "application/json", 403}, {"https://evil.example", "application/json", 403}, {"http://localhost:5173", "text/plain", 415}} {
		r := httptest.NewRequest("POST", BasePath+"/auth/login", strings.NewReader(`{"email":"a@b.c","password":"x"}`))
		r.Header.Set("Content-Type", tc.contentType)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}

func TestRolePermissionsAndScope(t *testing.T) {
	gl, im := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	superAdmin := Principal{Grants: []Grant{{Role: RoleSuperAdmin}}}
	auditor := Principal{Grants: []Grant{{Role: RoleAuditor}}}
	reviewerGL := Principal{Grants: []Grant{{Role: RoleReviewer, ProgramID: gl}}}
	manager := Principal{Grants: []Grant{{Role: RoleSelectionManager}}}
	for _, tc := range []struct {
		name string
		p    Principal
		perm Permission
		want bool
	}{
		{"super admin manages accounts", superAdmin, PermAccountsManage, true},
		{"super admin reads audit", superAdmin, PermAuditRead, true},
		{"super admin does not review", superAdmin, PermApplicationsReview, false},
		{"auditor reads audit", auditor, PermAuditRead, true},
		{"auditor reads accounts", auditor, PermAccountsRead, true},
		{"auditor never writes", auditor, PermAccountsManage, false},
		{"auditor never reviews", auditor, PermApplicationsReview, false},
		{"reviewer reviews", reviewerGL, PermApplicationsReview, true},
		{"reviewer has no audit", reviewerGL, PermAuditRead, false},
		{"reviewer does not rank", reviewerGL, PermRankingManage, false},
		{"manager validates selection", manager, PermSelectionValidate, true},
		{"manager has no accounts", manager, PermAccountsRead, false},
	} {
		if tc.p.Can(tc.perm) != tc.want {
			t.Fatal(tc.name)
		}
	}
	if !reviewerGL.CanFor(PermApplicationsReview, gl) || reviewerGL.CanFor(PermApplicationsReview, im) {
		t.Fatal("program scope ignored")
	}
	if all, ids := reviewerGL.Scope(PermApplicationsRead); all || !slices.Equal(ids, []string{gl}) {
		t.Fatal("scope", all, ids)
	}
	if all, _ := manager.Scope(PermApplicationsRead); !all {
		t.Fatal("unscoped grant must cover every program")
	}
	if !auditor.CanFor(PermApplicationsRead, im) {
		t.Fatal("global auditor limited")
	}
	for role := range GlobalRoles {
		if !KnownRole(role) {
			t.Fatal("unknown global role")
		}
	}
	// Phase 8 and 9 permissions are declared now and granted to a role.
	for _, perm := range []Permission{PermApplicationsRead, PermApplicationsReview, PermRankingRead, PermRankingManage, PermSelectionValidate} {
		granted := false
		for _, perms := range RolePermissions {
			granted = granted || slices.Contains(perms, perm)
		}
		if !granted || !ProgramScoped[perm] {
			t.Fatal("future permission not declared", perm)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	code, err := newRecoveryCode()
	if err != nil || len(code) != 11 || code[5] != '-' {
		t.Fatal(code, err)
	}
	if !looksLikeRecoveryCode(code) || looksLikeRecoveryCode("123456") {
		t.Fatal("recovery code shape")
	}
	if !bytes.Equal(hashRecoveryCode(code), hashRecoveryCode(strings.ToLower(strings.ReplaceAll(code, "-", " ")))) {
		t.Fatal("recovery code normalization")
	}
	other, _ := newRecoveryCode()
	if other == code {
		t.Fatal("recovery codes repeat")
	}
}
