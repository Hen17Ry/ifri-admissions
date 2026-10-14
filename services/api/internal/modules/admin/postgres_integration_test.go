//go:build integration

package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/platform/passwords"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
	"ifri-admissions/services/api/internal/platform/totp"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

const origin = "http://localhost:5173"

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// countedHasher wraps the real argon2id hasher (cheap parameters for speed) and counts calls.
type countedHasher struct {
	*passwords.Hasher
	calls atomic.Int32
}

func (h *countedHasher) Hash(ctx context.Context, p string) (string, error) {
	h.calls.Add(1)
	return h.Hasher.Hash(ctx, p)
}
func (h *countedHasher) Verify(ctx context.Context, e, p string) (bool, error) {
	h.calls.Add(1)
	return h.Hasher.Verify(ctx, e, p)
}
func (h *countedHasher) VerifyDummy(ctx context.Context, p string) error {
	h.calls.Add(1)
	return h.Hasher.VerifyDummy(ctx, p)
}

type env struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	provider *goose.Provider
	svc      *Service
	router   *Router
	mux      *http.ServeMux
	clock    *testClock
	hasher   *countedHasher
	drafts   *drafts.PostgresRepository
	secrets  map[string][]byte
	ips      atomic.Int32
}

func setup(t *testing.T, ctx context.Context) *env {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	suffix := make([]byte, 8)
	rand.Read(suffix)
	schema := "ifri_admin_test_" + hex.EncodeToString(suffix)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("cleanup failed")
		}
	})
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := stdlib.OpenDB(*pc.ConnConfig)
	t.Cleanup(func() { db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err = seeds.Development(ctx, pool, "test"); err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(&redis.Options{Addr: c.RedisAddress(), Password: c.RedisPassword})
	prefix := "ifri:test:" + schema + ":"
	t.Cleanup(func() {
		keys, _ := cache.Keys(context.Background(), prefix+"*").Result()
		if len(keys) > 0 {
			cache.Del(context.Background(), keys...)
		}
		cache.Close()
	})
	keys, err := secrets.Derive(bytes.Repeat([]byte{42}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	real, err := passwords.NewHasher(passwords.Params{MemoryKiB: 256, Time: 1, Threads: 1, SaltBytes: 16, KeyBytes: 32}, 4)
	if err != nil {
		t.Fatal(err)
	}
	clock := &testClock{t: time.Now().UTC().Truncate(time.Second)}
	hasher := &countedHasher{Hasher: real}
	svc := &Service{Pool: pool, Keys: keys, Hasher: hasher, Limiter: ratelimit.Redis{Client: cache, Prefix: prefix, Now: clock.Now},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), MFARequired: true, Idle: 30 * time.Minute, Absolute: 8 * time.Hour,
		ActivationTTL: 72 * time.Hour, Origin: origin, Now: clock.Now}
	mux := http.NewServeMux()
	router := Register(mux, svc, origin, false, svc.Logger)
	repo := drafts.NewRepository(pool)
	drafts.Register(mux, repo, time.Hour, origin, false, svc.Logger)
	return &env{t: t, ctx: ctx, pool: pool, provider: provider, svc: svc, router: router, mux: mux, clock: clock, hasher: hasher, drafts: repo, secrets: map[string][]byte{}}
}

// browser holds one admin cookie and CSRF token, from one documentation-range IP address.
type browser struct {
	e            *env
	cookie, csrf string
	ip           string
}

func (e *env) browser() *browser {
	return &browser{e: e, ip: fmt.Sprintf("192.0.2.%d", e.ips.Add(1))}
}

type response struct {
	status int
	header http.Header
	raw    string
	body   map[string]any
}

func (r response) code() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		return fmt.Sprint(e["code"])
	}
	return ""
}

func (b *browser) do(method, path string, payload any) response {
	b.e.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, BasePath+path, body)
	r.RemoteAddr = b.ip + ":40000"
	if method != "GET" {
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
	}
	if b.cookie != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: b.cookie})
	}
	if b.csrf != "" {
		r.Header.Set(CSRFHeader, b.csrf)
	}
	w := httptest.NewRecorder()
	b.e.mux.ServeHTTP(w, r)
	out := response{status: w.Code, header: w.Header(), raw: w.Body.String(), body: map[string]any{}}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			if c.MaxAge < 0 {
				b.cookie, b.csrf = "", ""
			} else {
				b.cookie = c.Value
			}
		}
	}
	if token, ok := out.body["csrfToken"].(string); ok {
		b.csrf = token
	}
	return out
}

func (b *browser) login(email, password string) response {
	return b.do("POST", "/auth/login", map[string]string{"email": email, "password": password})
}

func (e *env) code(email string) string {
	return totp.Code(e.secrets[email], totp.Step(e.clock.Now()))
}

// secondFactor completes enrolment or verification; the clock moves to a fresh TOTP step.
func (b *browser) secondFactor(email string, stage string) []string {
	b.e.t.Helper()
	b.e.clock.Advance(31 * time.Second)
	if stage == StageEnrollment {
		r := b.do("POST", "/auth/mfa/enrollment", map[string]any{})
		if r.status != 200 || !strings.HasPrefix(fmt.Sprint(r.body["qrSvg"]), "<svg") || r.header.Get("Cache-Control") != "no-store" {
			b.e.t.Fatalf("enrolment: %d %s", r.status, r.raw)
		}
		secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(fmt.Sprint(r.body["secret"]))
		if err != nil {
			b.e.t.Fatal(err)
		}
		b.e.secrets[email] = secret
		r = b.do("POST", "/auth/mfa/enrollment/confirm", map[string]string{"code": b.e.code(email)})
		if r.status != 200 || r.body["stage"] != StageActive {
			b.e.t.Fatalf("confirm: %d %s", r.status, r.raw)
		}
		codes := []string{}
		for _, c := range r.body["recoveryCodes"].([]any) {
			codes = append(codes, c.(string))
		}
		return codes
	}
	r := b.do("POST", "/auth/mfa/verify", map[string]string{"code": b.e.code(email)})
	if r.status != 200 || r.body["stage"] != StageActive {
		b.e.t.Fatalf("verify: %d %s", r.status, r.raw)
	}
	return nil
}

func (e *env) signIn(email, password string) *browser {
	e.t.Helper()
	b := e.browser()
	r := b.login(email, password)
	if r.status != 200 {
		e.t.Fatalf("login %s: %d %s", email, r.status, r.raw)
	}
	b.secondFactor(email, fmt.Sprint(r.body["stage"]))
	return b
}

func (e *env) activationToken(super *browser, email, name string, grants []GrantInput) (string, string) {
	e.t.Helper()
	r := super.do("POST", "/accounts", AccountInput{Email: email, DisplayName: name, Grants: grants})
	if r.status != 201 {
		e.t.Fatalf("create %s: %d %s", email, r.status, r.raw)
	}
	url := fmt.Sprint(r.body["activationUrl"])
	_, token, found := strings.Cut(url, "/admin/activation#")
	if !found || !strings.HasPrefix(url, origin) {
		e.t.Fatal("activation URL", url)
	}
	return token, r.body["account"].(map[string]any)["id"].(string)
}

const strong = "une phrase de passe assez longue"

func (e *env) account(super *browser, email string, grants ...GrantInput) (*browser, string) {
	e.t.Helper()
	token, id := e.activationToken(super, email, "Compte "+email, grants)
	if r := e.browser().do("POST", "/activation", map[string]string{"token": token, "password": strong}); r.status != 200 {
		e.t.Fatalf("activate: %d %s", r.status, r.raw)
	}
	return e.signIn(email, strong), id
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestPostgresAdmin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	e := setup(t, ctx)
	superEmail, superPassword := "super@ifri.example", "le premier super administrateur"

	t.Run("CLI bootstrap creates one super-administrator only", func(t *testing.T) {
		if _, err := e.svc.BootstrapSuperAdmin(ctx, superEmail, "Super", "123456789012"); err == nil || err.(*Problem).Code != "PASSWORD_TOO_COMMON" {
			t.Fatal("common password accepted", err)
		}
		if _, err := e.svc.BootstrapSuperAdmin(ctx, superEmail, "Super", superPassword); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.BootstrapSuperAdmin(ctx, "second@ifri.example", "Second", superPassword); err == nil {
			t.Fatal("second bootstrap accepted")
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_SUPER_ADMIN_BOOTSTRAPPED' AND actor_type='SYSTEM' AND actor_id IS NULL`) != 1 {
			t.Fatal("bootstrap not audited as SYSTEM")
		}
	})

	var super *browser
	var recovery []string
	t.Run("full authentication: password, enrolment, rotation, CSRF", func(t *testing.T) {
		b := e.browser()
		r := b.login(superEmail, superPassword)
		if r.status != 200 || r.body["stage"] != StageEnrollment || b.cookie == "" {
			t.Fatal(r.status, r.raw)
		}
		cookie := r.header.Get("Set-Cookie")
		for _, want := range []string{"ifri_admin=", "Path=/api/v1/admin", "HttpOnly", "SameSite=Strict"} {
			if !strings.Contains(cookie, want) {
				t.Fatal("cookie attribute missing", want, cookie)
			}
		}
		preAuth := b.cookie
		if r := b.do("GET", "/accounts", nil); r.status != 403 || r.code() != "ADMIN_STAGE_FORBIDDEN" {
			t.Fatal("pre-MFA session reached a business route", r.status)
		}
		csrf := b.csrf
		b.csrf = ""
		if r := b.do("POST", "/auth/mfa/enrollment", map[string]any{}); r.status != 403 || r.code() != "ADMIN_CSRF_INVALID" {
			t.Fatal("mutation without CSRF token", r.status)
		}
		b.csrf = csrf
		e.clock.Advance(31 * time.Second)
		r = b.do("POST", "/auth/mfa/enrollment", map[string]any{})
		secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(fmt.Sprint(r.body["secret"]))
		e.secrets[superEmail] = secret
		if r := b.do("POST", "/auth/mfa/enrollment/confirm", map[string]string{"code": "000000"}); r.status != 422 || r.code() != "ADMIN_MFA_INVALID" || b.cookie != preAuth {
			t.Fatal("wrong code", r.status, r.raw)
		}
		r = b.do("POST", "/auth/mfa/enrollment/confirm", map[string]string{"code": e.code(superEmail)})
		if r.status != 200 || r.body["stage"] != StageActive || b.cookie == preAuth {
			t.Fatal("confirm", r.status, r.raw)
		}
		for _, c := range r.body["recoveryCodes"].([]any) {
			recovery = append(recovery, c.(string))
		}
		if len(recovery) != 10 {
			t.Fatal("recovery codes", recovery)
		}
		stale := &browser{e: e, cookie: preAuth, ip: b.ip}
		if r := stale.do("GET", "/session", nil); r.status != 401 {
			t.Fatal("token not rotated at the second factor")
		}
		r = b.do("GET", "/session", nil)
		perms := fmt.Sprint(r.body["permissions"])
		if r.status != 200 || r.body["stage"] != StageActive || !strings.Contains(perms, "accounts.manage") || !strings.Contains(perms, "audit.read") {
			t.Fatal("session view", r.raw)
		}
		var sealed []byte
		var version int
		if err := e.pool.QueryRow(ctx, `SELECT key_version,ciphertext FROM admin_totp_secrets t JOIN admin_users u ON u.id=t.admin_user_id WHERE u.email=$1`, superEmail).Scan(&version, &sealed); err != nil || version != 1 || bytes.Contains(sealed, secret) {
			t.Fatal("TOTP secret stored in clear", err)
		}
		if e.count(`SELECT count(*) FROM admin_recovery_codes r JOIN admin_users u ON u.id=r.admin_user_id WHERE u.email=$1 AND used_at IS NULL`, superEmail) != 10 {
			t.Fatal("recovery codes not stored")
		}
		super = b
	})

	t.Run("TOTP: clock skew accepted, replay refused, recovery codes single use and regenerated", func(t *testing.T) {
		b := e.browser()
		if r := b.login(superEmail, superPassword); r.body["stage"] != StageVerification {
			t.Fatal(r.raw)
		}
		// Two steps later, the previous step is newer than the one used at enrolment.
		e.clock.Advance(61 * time.Second)
		previous := totp.Code(e.secrets[superEmail], totp.Step(e.clock.Now())-1)
		if r := b.do("POST", "/auth/mfa/verify", map[string]string{"code": previous}); r.status != 200 {
			t.Fatal("code of the previous step refused", r.raw)
		}
		replay := e.browser()
		replay.login(superEmail, superPassword)
		if r := replay.do("POST", "/auth/mfa/verify", map[string]string{"code": previous}); r.status != 422 {
			t.Fatal("replayed TOTP code accepted")
		}
		if r := replay.do("POST", "/auth/mfa/verify", map[string]string{"code": recovery[0]}); r.status != 200 {
			t.Fatal("recovery code refused", r.raw)
		}
		again := e.browser()
		again.login(superEmail, superPassword)
		if r := again.do("POST", "/auth/mfa/verify", map[string]string{"code": strings.ToLower(recovery[0])}); r.status != 422 {
			t.Fatal("recovery code used twice")
		}
		r := replay.do("POST", "/auth/recovery-codes", map[string]any{})
		fresh := r.body["recoveryCodes"].([]any)
		if r.status != 200 || len(fresh) != 10 {
			t.Fatal("regenerate", r.raw)
		}
		if r := again.do("POST", "/auth/mfa/verify", map[string]string{"code": recovery[1]}); r.status != 422 {
			t.Fatal("old recovery code still valid after regeneration")
		}
		if r := again.do("POST", "/auth/mfa/verify", map[string]string{"code": fresh[0].(string)}); r.status != 200 {
			t.Fatal("new recovery code refused")
		}
	})

	t.Run("no account enumeration: same answer, one argon2 each", func(t *testing.T) {
		b := e.browser()
		before := e.hasher.calls.Load()
		unknown := b.login("nobody@ifri.example", "un mot de passe quelconque")
		afterUnknown := e.hasher.calls.Load()
		wrong := b.login(superEmail, "un mot de passe quelconque")
		afterWrong := e.hasher.calls.Load()
		if unknown.status != 401 || unknown.raw != wrong.raw || unknown.code() != "ADMIN_LOGIN_FAILED" {
			t.Fatal("responses differ", unknown.raw, wrong.raw)
		}
		if afterUnknown-before != 1 || afterWrong-afterUnknown != 1 {
			t.Fatal("argon2 cost differs", afterUnknown-before, afterWrong-afterUnknown)
		}
	})

	reviewer, reviewerID := e.account(super, "reviewer.gl@ifri.example", GrantInput{Role: RoleReviewer, Program: "GL"})
	manager, _ := e.account(super, "manager@ifri.example", GrantInput{Role: RoleSelectionManager})
	auditor, _ := e.account(super, "auditor@ifri.example", GrantInput{Role: RoleAuditor})

	t.Run("activation link: hashed, fragment, single use, expiry, password policy", func(t *testing.T) {
		token, id := e.activationToken(super, "pending@ifri.example", "En attente", []GrantInput{{Role: RoleAuditor}})
		if e.count(`SELECT count(*) FROM admin_activation_tokens WHERE token_hash=sha256(convert_to($1,'UTF8'))`, token) != 1 {
			t.Fatal("token not stored as SHA-256")
		}
		anon := e.browser()
		if r := anon.do("POST", "/activation/inspect", map[string]string{"token": token}); r.status != 200 || r.body["email"] != "pending@ifri.example" {
			t.Fatal(r.raw)
		}
		if r := anon.do("POST", "/activation", map[string]string{"token": token, "password": "Password1234"}); r.status != 422 || r.code() != "PASSWORD_TOO_COMMON" {
			t.Fatal("common password accepted", r.raw)
		}
		if r := anon.do("POST", "/activation", map[string]string{"token": token, "password": "pending@ifri.example"}); r.status != 422 {
			t.Fatal("email as password accepted")
		}
		if r := anon.do("POST", "/activation", map[string]string{"token": token, "password": strong}); r.status != 200 {
			t.Fatal(r.raw)
		}
		if r := anon.do("POST", "/activation", map[string]string{"token": token, "password": strong}); r.status != 410 {
			t.Fatal("token reused")
		}
		expiring, _ := e.activationToken(super, "late@ifri.example", "En retard", []GrantInput{{Role: RoleAuditor}})
		e.clock.Advance(73 * time.Hour)
		if r := anon.do("POST", "/activation/inspect", map[string]string{"token": expiring}); r.status != 410 {
			t.Fatal("expired token accepted")
		}
		// The super-administrator session expired with the clock: sign in again.
		super = e.signIn(superEmail, superPassword)
		r := super.do("POST", "/accounts/"+e.accountID("late@ifri.example")+"/activation-link", map[string]any{})
		if r.status != 200 {
			t.Fatal(r.raw)
		}
		_, renewed, _ := strings.Cut(fmt.Sprint(r.body["activationUrl"]), "#")
		if anon.do("POST", "/activation/inspect", map[string]string{"token": renewed}).status != 200 {
			t.Fatal("reissued link refused")
		}
		_ = id
		reviewer = e.signIn("reviewer.gl@ifri.example", strong)
		manager = e.signIn("manager@ifri.example", strong)
		auditor = e.signIn("auditor@ifri.example", strong)
	})

	t.Run("permission matrix over every registered route", func(t *testing.T) {
		missing := "00000000-0000-4000-8000-000000000000"
		roles := map[string]*browser{"SUPER_ADMIN": super, "REVIEWER": reviewer, "SELECTION_MANAGER": manager, "AUDITOR": auditor}
		principals := map[string]Principal{"SUPER_ADMIN": {Grants: []Grant{{Role: RoleSuperAdmin}}}, "REVIEWER": {Grants: []Grant{{Role: RoleReviewer, ProgramID: "x"}}},
			"SELECTION_MANAGER": {Grants: []Grant{{Role: RoleSelectionManager}}}, "AUDITOR": {Grants: []Grant{{Role: RoleAuditor}}}}
		for _, route := range e.router.Routes() {
			path := strings.TrimPrefix(strings.ReplaceAll(route.Path, "{id}", missing), BasePath)
			switch route.Permission {
			case PermPublic, PermAnySession:
				continue
			case PermEnrollment, PermVerification:
				// Second-factor routes are refused to an already active session.
				if r := super.do(route.Method, path, map[string]any{}); r.status != 403 || r.code() != "ADMIN_STAGE_FORBIDDEN" {
					t.Fatalf("%s %s from an active session: %d", route.Method, route.Path, r.status)
				}
				continue
			}
			if r := e.browser().do(route.Method, path, map[string]any{}); r.status != 401 {
				t.Fatalf("%s %s without session: %d", route.Method, route.Path, r.status)
			}
			if route.Path == BasePath+"/auth/recovery-codes" {
				continue // allowed for everyone; it would rotate their codes
			}
			for role, b := range roles {
				allowed := route.Permission == PermSelf || principals[role].Can(route.Permission)
				r := b.do(route.Method, path, map[string]any{})
				if allowed && (r.status == 401 || r.status == 403) || !allowed && (r.status != 403 || r.code() != "ADMIN_FORBIDDEN") {
					t.Fatalf("%s on %s %s: %d %s (allowed=%v)", role, route.Method, route.Path, r.status, r.raw, allowed)
				}
			}
		}
	})

	t.Run("program scope of a reviewer limited to GL", func(t *testing.T) {
		session, err := e.svc.Authenticate(ctx, reviewer.cookie)
		if err != nil {
			t.Fatal(err)
		}
		var gl, im string
		e.pool.QueryRow(ctx, `SELECT (SELECT id::text FROM programs WHERE code='GL'),(SELECT id::text FROM programs WHERE code='IM')`).Scan(&gl, &im)
		if !session.Principal.CanFor(PermApplicationsReview, gl) || session.Principal.CanFor(PermApplicationsReview, im) {
			t.Fatal("scope not enforced")
		}
		if all, ids := session.Principal.Scope(PermApplicationsRead); all || !slices.Equal(ids, []string{gl}) {
			t.Fatal("scope list", all, ids)
		}
		for _, grants := range [][]GrantInput{{{Role: RoleSuperAdmin, Program: "GL"}}, {{Role: RoleReviewer, Program: "NOPE"}}, {{Role: "ROOT"}}, {}} {
			if r := super.do("POST", "/accounts", AccountInput{Email: "scope@ifri.example", DisplayName: "Scope", Grants: grants}); r.status != 422 {
				t.Fatal("invalid grants accepted", grants, r.raw)
			}
		}
	})

	t.Run("candidate and admin sessions never cross", func(t *testing.T) {
		token, hash, _ := drafts.NewSecret()
		if _, err := e.drafts.Create(ctx, "DEV_LOCAL", hash, time.Hour); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", BasePath+"/session", nil)
		r.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: token})
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		w := httptest.NewRecorder()
		e.mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("candidate session opened the admin API", w.Code)
		}
		r = httptest.NewRequest("GET", drafts.CookiePath+"/current", nil)
		r.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: super.cookie})
		r.AddCookie(&http.Cookie{Name: CookieName, Value: super.cookie})
		w = httptest.NewRecorder()
		e.mux.ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Body.String(), "DRAFT_SESSION_REQUIRED") {
			t.Fatal("admin session opened a candidate route", w.Code)
		}
	})

	t.Run("revocation: disable, role change, MFA reset, password change, logout", func(t *testing.T) {
		if r := super.do("POST", "/accounts/"+reviewerID+"/disable", map[string]any{}); r.status != 200 || r.body["status"] != "DISABLED" {
			t.Fatal(r.raw)
		}
		if reviewer.do("GET", "/session", nil).status != 401 {
			t.Fatal("disabled account kept its session")
		}
		unknown := e.browser().login("nobody@ifri.example", strong)
		disabled := e.browser().login("reviewer.gl@ifri.example", strong)
		if disabled.status != 401 || disabled.raw != unknown.raw {
			t.Fatal("disabled account distinguishable", disabled.raw)
		}
		if r := super.do("POST", "/accounts/"+reviewerID+"/enable", map[string]any{}); r.status != 200 || r.body["status"] != "ACTIVE" {
			t.Fatal(r.raw)
		}
		reviewer = e.signIn("reviewer.gl@ifri.example", strong)
		if r := super.do("PUT", "/accounts/"+reviewerID+"/grants", map[string]any{"grants": []GrantInput{{Role: RoleReviewer, Program: "GL"}, {Role: RoleReviewer, Program: "IM"}}}); r.status != 200 {
			t.Fatal(r.raw)
		}
		if reviewer.do("GET", "/session", nil).status != 401 {
			t.Fatal("role change kept the session")
		}
		reviewer = e.signIn("reviewer.gl@ifri.example", strong)
		if r := super.do("POST", "/accounts/"+reviewerID+"/mfa/reset", map[string]any{}); r.status != 200 || r.body["mfaEnabled"] != false {
			t.Fatal(r.raw)
		}
		if reviewer.do("GET", "/session", nil).status != 401 {
			t.Fatal("MFA reset kept the session")
		}
		b := e.browser()
		if r := b.login("reviewer.gl@ifri.example", strong); r.body["stage"] != StageEnrollment {
			t.Fatal("enrolment not required after reset", r.raw)
		}
		b.secondFactor("reviewer.gl@ifri.example", StageEnrollment)
		other := e.signIn("reviewer.gl@ifri.example", strong)
		if r := b.do("POST", "/auth/password", map[string]string{"currentPassword": "faux mot de passe actuel", "newPassword": "un nouveau mot de passe solide"}); r.status != 422 || r.code() != "ADMIN_PASSWORD_INVALID" || b.cookie == "" {
			t.Fatal("wrong current password", r.raw)
		}
		if r := b.do("POST", "/auth/password", map[string]string{"currentPassword": strong, "newPassword": "qwertyuiopasdfgh"}); r.status != 422 || r.code() != "PASSWORD_TOO_COMMON" {
			t.Fatal("common new password", r.raw)
		}
		if r := b.do("POST", "/auth/password", map[string]string{"currentPassword": strong, "newPassword": "un nouveau mot de passe solide"}); r.status != 200 || b.cookie != "" {
			t.Fatal("password change", r.raw)
		}
		if other.do("GET", "/session", nil).status != 401 {
			t.Fatal("password change kept another session")
		}
		if e.browser().login("reviewer.gl@ifri.example", strong).status != 401 {
			t.Fatal("old password still valid")
		}
		reviewer = e.signIn("reviewer.gl@ifri.example", "un nouveau mot de passe solide")
		if r := reviewer.do("POST", "/auth/logout", map[string]any{}); r.status != 200 || reviewer.cookie != "" {
			t.Fatal("logout", r.raw)
		}
	})

	t.Run("last super-administrator and self-disable are refused", func(t *testing.T) {
		superID := e.accountID(superEmail)
		if r := super.do("POST", "/accounts/"+superID+"/disable", map[string]any{}); r.status != 409 || r.code() != "ADMIN_SELF_ACTION_FORBIDDEN" {
			t.Fatal(r.raw)
		}
		if r := super.do("PUT", "/accounts/"+superID+"/grants", map[string]any{"grants": []GrantInput{{Role: RoleAuditor}}}); r.status != 409 || r.code() != "ADMIN_LAST_SUPER_ADMIN" {
			t.Fatal(r.raw)
		}
		second, secondID := e.account(super, "second.super@ifri.example", GrantInput{Role: RoleSuperAdmin})
		if r := second.do("PUT", "/accounts/"+secondID+"/grants", map[string]any{"grants": []GrantInput{{Role: RoleAuditor}}}); r.status != 200 {
			t.Fatal("demotion with another super-administrator", r.raw)
		}
		if second.do("GET", "/session", nil).status != 401 {
			t.Fatal("own role change kept the session")
		}
	})

	t.Run("session expiry: pre-authentication, idle and absolute", func(t *testing.T) {
		b := e.browser()
		b.login("manager@ifri.example", strong)
		e.clock.Advance(11 * time.Minute)
		if r := b.do("GET", "/session", nil); r.status != 401 || r.code() != "ADMIN_SESSION_EXPIRED" {
			t.Fatal("pre-authentication session outlived 10 minutes", r.raw)
		}
		b = e.signIn("manager@ifri.example", strong)
		e.clock.Advance(31 * time.Minute)
		if r := b.do("GET", "/session", nil); r.status != 401 || r.code() != "ADMIN_SESSION_EXPIRED" {
			t.Fatal("idle session kept", r.raw)
		}
		b = e.signIn("manager@ifri.example", strong)
		for range 23 {
			e.clock.Advance(20 * time.Minute)
			if r := b.do("GET", "/session", nil); r.status != 200 {
				t.Fatal("active session expired early", r.raw)
			}
		}
		e.clock.Advance(21 * time.Minute)
		if r := b.do("GET", "/session", nil); r.status != 401 || r.code() != "ADMIN_SESSION_EXPIRED" {
			t.Fatal("absolute expiry not enforced", r.raw)
		}
		if e.count(`SELECT count(*) FROM admin_sessions WHERE revoked_reason='EXPIRED'`) < 3 {
			t.Fatal("expired sessions not revoked")
		}
		super = e.signIn(superEmail, superPassword)
	})

	t.Run("rate limiting: per account then per IP, before argon2", func(t *testing.T) {
		b := e.browser()
		locked := false
		for range 40 {
			r := b.login("auditor@ifri.example", "pas le bon mot de passe")
			if r.status == 429 {
				retry := r.header.Get("Retry-After")
				if retry == "" {
					t.Fatal("Retry-After missing")
				}
				var seconds int
				fmt.Sscan(retry, &seconds)
				if seconds >= 14*60 {
					locked = true
					break
				}
				e.clock.Advance(time.Duration(seconds) * time.Second)
				continue
			}
			if r.status != 401 {
				t.Fatal(r.raw)
			}
		}
		if !locked {
			t.Fatal("account never locked")
		}
		calls := e.hasher.calls.Load()
		if r := e.browser().login("auditor@ifri.example", strong); r.status != 429 || e.hasher.calls.Load() != calls {
			t.Fatal("locked account computed argon2 or accepted the right password", r.status)
		}
		e.clock.Advance(16 * time.Minute)
		if r := e.browser().login("auditor@ifri.example", strong); r.status != 200 {
			t.Fatal("lock never released", r.raw)
		}
		// Invented emails from one IP: blocked requests never compute the dummy argon2.
		flood := e.browser()
		blocked := 0
		for i := range 45 {
			before := e.hasher.calls.Load()
			r := flood.login(fmt.Sprintf("invented-%d@example.invalid", i), "un mot de passe quelconque")
			spent := e.hasher.calls.Load() - before
			if r.status == 429 {
				blocked++
				if spent != 0 {
					t.Fatal("argon2 computed for a blocked request")
				}
			} else if spent != 1 {
				t.Fatal("unexpected argon2 count", spent)
			}
		}
		if blocked < 15 {
			t.Fatal("IP not limited", blocked)
		}
	})

	t.Run("audit: every sensitive action, no clear identifier, IP or password", func(t *testing.T) {
		if err := audit.RecordAccess(ctx, e.pool, e.accountID(superEmail), "APPLICATION", "00000000-0000-4000-8000-000000000001", "REVIEW"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.ResetMFAByEmail(ctx, "manager@ifri.example"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.ReactivateByEmail(ctx, "auditor@ifri.example"); err == nil {
			t.Fatal("reactivation of an active account accepted")
		}
		// Forgotten password, even of a super-administrator: server-side link, MFA kept.
		forgot := e.signIn(superEmail, superPassword)
		url, _, err := e.svc.ResetPasswordByEmail(ctx, superEmail)
		if err != nil {
			t.Fatal(err)
		}
		if forgot.do("GET", "/session", nil).status != 401 || e.browser().login(superEmail, superPassword).status != 401 {
			t.Fatal("password reset kept the old password or a session")
		}
		_, token, _ := strings.Cut(url, "#")
		if r := e.browser().do("POST", "/activation", map[string]string{"token": token, "password": "un mot de passe retrouvé solide"}); r.status != 200 {
			t.Fatal(r.raw)
		}
		if r := e.browser().login(superEmail, "un mot de passe retrouvé solide"); r.body["stage"] != StageVerification {
			t.Fatal("second factor lost by the password reset", r.raw)
		}
		superPassword = "un mot de passe retrouvé solide"
		for _, event := range []string{"ADMIN_SUPER_ADMIN_BOOTSTRAPPED", "ADMIN_LOGIN_PASSWORD_ACCEPTED", "ADMIN_LOGIN_SUCCEEDED", "ADMIN_LOGIN_FAILED", "ADMIN_LOGIN_LOCKED",
			"ADMIN_MFA_FAILED", "ADMIN_MFA_ENROLLED", "ADMIN_RECOVERY_CODE_USED", "ADMIN_RECOVERY_CODES_REGENERATED", "ADMIN_LOGOUT", "ADMIN_PASSWORD_CHANGED",
			"ADMIN_PASSWORD_CHANGE_FAILED", "ADMIN_ACCOUNT_CREATED", "ADMIN_ACTIVATION_LINK_ISSUED", "ADMIN_ACCOUNT_ACTIVATED", "ADMIN_ACTIVATION_FAILED",
			"ADMIN_ACCOUNT_DISABLED", "ADMIN_ACCOUNT_ENABLED", "ADMIN_ROLES_CHANGED", "ADMIN_MFA_RESET", "ADMIN_PASSWORD_RESET", audit.AccessEvent} {
			if e.count(`SELECT count(*) FROM audit_events WHERE event_type=$1`, event) == 0 {
				t.Fatal("not audited:", event)
			}
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_MFA_RESET' AND actor_type='SYSTEM' AND metadata->>'via'='CLI'`) != 1 ||
			e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_MFA_RESET' AND actor_type='ADMIN' AND actor_id IS NOT NULL`) != 1 {
			t.Fatal("MFA resets not attributed")
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_LOGIN_FAILED' AND actor_type='SYSTEM' AND length(metadata->>'identifierHash')=64 AND length(metadata->>'ipHash')=64`) == 0 {
			t.Fatal("failed login without hashed identifier")
		}
		rows, err := e.pool.Query(ctx, `SELECT metadata::text FROM audit_events`)
		if err != nil {
			t.Fatal(err)
		}
		texts, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, text := range texts {
			for _, secret := range []string{"@ifri.example", "@example.invalid", "192.0.2.", superPassword, strong, "un mot de passe quelconque", "pas le bon"} {
				if strings.Contains(text, secret) {
					t.Fatal("audit metadata leaks", secret)
				}
			}
		}
		page, err := e.svc.Audit(ctx, AuditFilter{EventType: "ADMIN_LOGIN_FAILED", Limit: 2})
		if err != nil || len(page.Events) != 2 || page.NextCursor == "" {
			t.Fatal("audit page", err)
		}
		next, err := e.svc.Audit(ctx, AuditFilter{EventType: "ADMIN_LOGIN_FAILED", Limit: 2, Cursor: page.NextCursor})
		if err != nil || len(next.Events) == 0 || next.Events[0].ID == page.Events[1].ID || !next.Events[0].OccurredAt.Before(page.Events[1].OccurredAt.Add(time.Nanosecond)) {
			t.Fatal("audit keyset pagination", err)
		}
		auditor = e.signIn("auditor@ifri.example", strong)
		r := auditor.do("GET", "/audit-events?eventType=ADMIN_ACCOUNT_CREATED", nil)
		if r.status != 200 || !strings.Contains(r.raw, `"targetLabel":"reviewer.gl@ifri.example"`) {
			t.Fatal("audit API filter", r.status, r.raw)
		}
		for _, event := range r.body["events"].([]any) {
			if event.(map[string]any)["eventType"] != "ADMIN_ACCOUNT_CREATED" {
				t.Fatal("filter ignored")
			}
		}
		if r := auditor.do("GET", "/audit-events?eventType=DROP_TABLE", nil); r.status != 400 {
			t.Fatal("unknown event type accepted")
		}
		if e.count(`SELECT count(*) FROM admin_users`) == 0 {
			t.Fatal("no accounts")
		}
		if _, err := e.pool.Exec(ctx, `DELETE FROM admin_users WHERE email='auditor@ifri.example'`); err == nil {
			t.Fatal("admin account deleted")
		}
		if _, err := e.pool.Exec(ctx, `UPDATE admin_role_assignments SET role='SUPER_ADMIN'`); err == nil {
			t.Fatal("role assignment edited in place")
		}
		if _, err := e.provider.Down(ctx); err == nil || !strings.Contains(err.Error(), "rollback refused") {
			t.Fatal("migration 00007 rollback accepted with accounts", err)
		}
	})
}

func (e *env) accountID(email string) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(e.ctx, `SELECT id::text FROM admin_users WHERE email=$1`, email).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestPostgresAdminMigrationRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	e := setup(t, ctx)
	if _, err := e.provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name LIKE 'admin\_%'`) != 0 {
		t.Fatal("rollback left admin tables")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE admission_campaigns SET numbering_year=numbering_year WHERE code='DEV_LOCAL'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.BootstrapSuperAdmin(ctx, "roundtrip@ifri.example", "Aller-retour", strong); err != nil {
		t.Fatal(err)
	}
}

func TestRedisRateLimiterPolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e := setup(t, ctx)
	limiter := e.svc.Limiter
	p := ratelimit.Policy{Name: "unit", MaxAttempts: 100, Window: time.Hour, FreeFailures: 2, BaseDelay: time.Second, MaxDelay: 8 * time.Second, LockAfter: 6, LockFor: time.Hour}
	delays := []time.Duration{}
	for range 6 {
		if retry, err := limiter.Attempt(ctx, p, "k"); err != nil || retry != 0 {
			t.Fatal("blocked too early", retry, err)
		}
		d, err := limiter.Failure(ctx, p, "k")
		if err != nil {
			t.Fatal(err)
		}
		delays = append(delays, d)
		e.clock.Advance(d)
	}
	if !slices.Equal(delays, []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second, time.Hour}) {
		t.Fatal("progressive delay", delays)
	}
	if retry, _ := limiter.Attempt(ctx, p, "other"); retry != 0 {
		t.Fatal("keys not independent")
	}
	if err := limiter.Reset(ctx, p, "k"); err != nil {
		t.Fatal(err)
	}
	if retry, _ := limiter.Attempt(ctx, p, "k"); retry != 0 {
		t.Fatal("reset ignored")
	}
	cap := ratelimit.Policy{Name: "cap", MaxAttempts: 3, Window: time.Hour, FreeFailures: 100, LockAfter: 100, LockFor: time.Minute}
	for i := range 4 {
		retry, _ := limiter.Attempt(ctx, cap, "k")
		if (i < 3) != (retry == 0) {
			t.Fatal("attempt cap", i, retry)
		}
	}
}
