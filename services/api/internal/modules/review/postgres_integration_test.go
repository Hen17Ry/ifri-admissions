//go:build integration

package review

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/modules/submissions"
	"ifri-admissions/services/api/internal/platform/passwords"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
	"ifri-admissions/services/api/internal/platform/storage"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

const origin = "http://localhost:5173"

// memoryObjects is an in-memory private bucket.
type memoryObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

type reader struct{ *bytes.Reader }

func (reader) Close() error { return nil }

func (m *memoryObjects) Get(_ context.Context, key string) (storage.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objects[key]
	if !ok {
		return storage.Object{}, errors.New("missing")
	}
	return storage.Object{Body: reader{bytes.NewReader(body)}, Size: int64(len(body)), ContentType: m.types[key]}, nil
}

func (m *memoryObjects) PutContent(_ context.Context, key string, body io.Reader, _ int64, contentType string) error {
	raw, err := io.ReadAll(body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key], m.types[key] = raw, contentType
	return err
}

type env struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	provider *goose.Provider
	svc      *Service
	admin    *admin.Service
	router   *admin.Router
	mux      *http.ServeMux
	internal *http.ServeMux
	objects  *memoryObjects
	cache    *redis.Client
}

func setup(t *testing.T, ctx context.Context) *env {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	root, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	suffix := make([]byte, 8)
	rand.Read(suffix)
	schema := "ifri_review_test_" + hex.EncodeToString(suffix)
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE") })
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pc.MaxConns = 16
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
	stream := "ifri:test:proofs:" + schema
	t.Cleanup(func() {
		keys, _ := cache.Keys(context.Background(), "ifri:test:"+schema+":*").Result()
		cache.Del(context.Background(), append(keys, stream)...)
		cache.Close()
	})
	keys, _ := secrets.Derive(bytes.Repeat([]byte{7}, 32), 1)
	hasher, _ := passwords.NewHasher(passwords.Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltBytes: 16, KeyBytes: 32}, 2)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := ratelimit.Redis{Client: cache, Prefix: "ifri:test:" + schema + ":"}
	adminSvc := &admin.Service{Pool: pool, Keys: keys, Hasher: hasher, Limiter: limiter, Logger: quiet, MFARequired: true, Idle: 30 * time.Minute, Absolute: 8 * time.Hour, ActivationTTL: time.Hour, Origin: origin}
	objects := &memoryObjects{objects: map[string][]byte{}, types: map[string]string{}}
	svc := &Service{Pool: pool, Objects: objects, Cache: cache, Logger: quiet, Stream: stream}
	mux := http.NewServeMux()
	router := admin.Register(mux, adminSvc, origin, false, quiet)
	Register(router, svc)
	internal := http.NewServeMux()
	svc.RegisterInternal(internal, strings.Repeat("s", 40))
	return &env{t: t, ctx: ctx, pool: pool, provider: provider, svc: svc, admin: adminSvc, router: router, mux: mux, internal: internal, objects: objects, cache: cache}
}

// actor is an active account with an open session (authentication is covered by the admin tests).
type actor struct {
	e         *env
	principal admin.Principal
	token     string
	csrf      string
}

func (e *env) actor(name string, role admin.Role) *actor {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(e.ctx, `INSERT INTO admin_users(email,display_name,password_hash,status,mfa_enabled_at) VALUES($1,$2,'$argon2id$v=19$m=64,t=1,p=1$AA$AA','ACTIVE',now()) RETURNING id::text`,
		strings.ToLower(strings.ReplaceAll(name, " ", "."))+"@ifri.example", name).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO admin_role_assignments(admin_user_id,role) VALUES($1::uuid,$2)`, id, role); err != nil {
		e.t.Fatal(err)
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO admin_sessions(admin_user_id,token_hash,stage,absolute_expires_at) VALUES($1::uuid,$2,'ACTIVE',now()+interval '1 hour')`, id, sum[:]); err != nil {
		e.t.Fatal(err)
	}
	return &actor{e: e, principal: admin.Principal{UserID: id, DisplayName: name, Grants: []admin.Grant{{Role: role}}}, token: token, csrf: e.admin.Keys.CSRF(sum[:])}
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

func (a *actor) do(method, path string, payload any) response {
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, admin.BasePath+path, body)
	if method != "GET" {
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(admin.CSRFHeader, a.csrf)
	}
	if a.token != "" {
		r.AddCookie(&http.Cookie{Name: admin.CookieName, Value: a.token})
	}
	w := httptest.NewRecorder()
	a.e.mux.ServeHTTP(w, r)
	out := response{status: w.Code, header: w.Header(), raw: w.Body.String(), body: map[string]any{}}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) firstDossier(where string) (string, int) {
	e.t.Helper()
	var id string
	var version int
	if err := e.pool.QueryRow(e.ctx, `SELECT r.application_id::text,r.version FROM application_reviews r JOIN applications a ON a.id=r.application_id WHERE `+where+` ORDER BY a.application_number LIMIT 1`).Scan(&id, &version); err != nil {
		e.t.Fatal(err)
	}
	return id, version
}

func version(e *env, id string) int {
	return e.count(`SELECT version FROM application_reviews WHERE application_id=$1::uuid`, id)
}

func problemCode(err error) string {
	var p *admin.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func percentile(durations []time.Duration, p float64) time.Duration {
	sorted := slices.Clone(durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[int(float64(len(sorted)-1)*p)]
}

func TestPostgresReview(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	e := setup(t, ctx)
	started := time.Now()
	n, err := GenerateSynthetic(ctx, e.pool, Synthetic{Campaign: "DEV_LOCAL", Count: 5000})
	if err != nil || n != 5000 {
		t.Fatal("synthetic data", n, err)
	}
	t.Logf("synthetic generation: 5000 submitted applications in %s", time.Since(started).Round(time.Millisecond))

	superAdmin := e.actor("Admin Technique", admin.RoleSuperAdmin)
	agentA := e.actor("Agent A", admin.RoleReviewer)
	agentB := e.actor("Agent B", admin.RoleReviewer)
	lead := e.actor("Responsable SE", admin.RoleReviewLead)
	deputy := e.actor("Directeur Adjoint", admin.RoleDeputyDirector)
	director := e.actor("Directeur", admin.RoleDirector)

	t.Run("synthetic data passes every submission constraint", func(t *testing.T) {
		if e.count(`SELECT count(*) FROM application_reviews WHERE status='TO_REVIEW'`) != 5000 ||
			e.count(`SELECT count(*) FROM application_review_events WHERE event_type='CREATED'`) != 5000 {
			t.Fatal("reviews not created at submission")
		}
		for _, code := range []string{"DUPLICATE_EMAIL", "DUPLICATE_PHONE", "DUPLICATE_TRANSCRIPT", "SERIES_UNREADABLE"} {
			if e.count(`SELECT count(*) FROM application_submission_flags WHERE code=$1`, code) == 0 {
				t.Fatal("no synthetic flag", code)
			}
		}
		if e.count(`SELECT count(*) FROM candidates WHERE email NOT LIKE '%@example.invalid'`) != 0 {
			t.Fatal("synthetic data outside the reserved domain")
		}
	})

	t.Run("list: server-side paging, sorting, filters and accent-insensitive search", func(t *testing.T) {
		page, err := e.svc.List(ctx, agentA.principal, Filter{PageSize: 25})
		if err != nil || page.Total != 5000 || len(page.Items) != 25 {
			t.Fatal(page.Total, err)
		}
		item := page.Items[0]
		if item.Average == nil || len(item.Average.Display) < 4 || strings.Count(item.Average.Display, ".") != 1 || len(strings.Split(item.Average.Display, ".")[1]) != 2 {
			t.Fatalf("average display %+v", item.Average)
		}
		sorted, _ := e.svc.List(ctx, agentA.principal, Filter{Sort: "average", Direction: "desc", PageSize: 100})
		for i := 1; i < len(sorted.Items); i++ {
			if sorted.Items[i-1].Average.Decimal < sorted.Items[i].Average.Decimal {
				t.Fatal("not sorted by average")
			}
		}
		byName, _ := e.svc.List(ctx, agentA.principal, Filter{Query: "GBEDJI elodie", PageSize: 100})
		if byName.Total == 0 {
			t.Fatal("accent-insensitive search found nothing")
		}
		// Every word is a substring of the folded name: "gbedji" also matches "Houngbédji".
		exact := false
		for _, it := range byName.Items {
			if (it.LastName != "Gbèdji" && it.LastName != "Houngbédji") || it.FirstNames != "Élodie" {
				t.Fatal("search returned", it.LastName, it.FirstNames)
			}
			exact = exact || it.LastName == "Gbèdji"
		}
		if !exact {
			t.Fatal("accented name not found from its unaccented form")
		}
		number, _ := e.svc.List(ctx, agentA.principal, Filter{Query: strings.ToLower(item.Number)})
		if number.Total != 1 || number.Items[0].ID != item.ID {
			t.Fatal("number search")
		}
		var email string
		e.pool.QueryRow(ctx, `SELECT email FROM application_submissions WHERE application_id=$1::uuid`, item.ID).Scan(&email)
		byEmail, _ := e.svc.List(ctx, agentA.principal, Filter{Query: strings.ToUpper(email)})
		if byEmail.Total < 1 || !slices.ContainsFunc(byEmail.Items, func(i ListItem) bool { return i.ID == item.ID }) {
			t.Fatal("email search")
		}
		flagged, _ := e.svc.List(ctx, agentA.principal, Filter{Flag: "DUPLICATE_EMAIL"})
		if flagged.Total != e.count(`SELECT count(DISTINCT application_id) FROM application_submission_flags WHERE code='DUPLICATE_EMAIL'`) {
			t.Fatal("flag filter")
		}
		gl, _ := e.svc.List(ctx, agentA.principal, Filter{Program: "GL", Qualification: "BAC_D", Status: StatusToReview})
		if gl.Total == 0 || gl.Total != e.count(`SELECT count(*) FROM application_submissions s JOIN programs p ON p.id=s.program_id JOIN origin_qualifications q ON q.id=s.origin_qualification_id WHERE p.code='GL' AND q.code='BAC_D'`) {
			t.Fatal("program and series filters")
		}
		if raw, _ := json.Marshal(page); strings.Contains(string(raw), "@example.invalid") || strings.Contains(string(raw), "+22990") {
			t.Fatal("contact details in the list")
		}
		if _, err := e.svc.List(ctx, agentA.principal, Filter{Sort: "email"}); problemCode(err) != "INVALID_PARAMETERS" {
			t.Fatal("unknown sort accepted")
		}
	})

	t.Run("list performance on 5000 dossiers", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			f    Filter
		}{
			{"default (date)", Filter{}},
			{"average descending", Filter{Sort: "average", Direction: "desc"}},
			{"name ascending, page 100", Filter{Sort: "name", Direction: "asc", Page: 100}},
			{"accent-insensitive name", Filter{Query: "hounkpe sena"}},
			{"number prefix", Filter{Query: "IFRI-"}},
			{"program + series + status", Filter{Program: "GL", Qualification: "BAC_C", Status: StatusToReview}},
			{"flag", Filter{Flag: "ANY"}},
		} {
			durations := []time.Duration{}
			for range 20 {
				begin := time.Now()
				if _, err := e.svc.List(ctx, agentA.principal, tc.f); err != nil {
					t.Fatal(err)
				}
				durations = append(durations, time.Since(begin))
			}
			p50, p95 := percentile(durations, 0.5), percentile(durations, 0.95)
			t.Logf("PERF list %-28s p50=%6.1f ms p95=%6.1f ms", tc.name, float64(p50.Microseconds())/1000, float64(p95.Microseconds())/1000)
			if p95 > 2*time.Second {
				t.Fatal("list too slow", tc.name, p95)
			}
		}
	})

	t.Run("roles × actions over HTTP", func(t *testing.T) {
		id, v := e.firstDossier(`r.status='TO_REVIEW'`)
		for name, a := range map[string]*actor{"super-admin": superAdmin} {
			for _, path := range []string{"/applications", "/applications/" + id, "/corrections", "/review/reference"} {
				if r := a.do("GET", path, nil); r.status != 403 || r.code() != "ADMIN_FORBIDDEN" {
					t.Fatalf("%s reads %s: %d", name, path, r.status)
				}
			}
		}
		if r := director.do("GET", "/applications/"+id, nil); r.status != 200 || r.body["applicationNumber"] == nil {
			t.Fatal("director cannot read", r.raw)
		}
		if r := director.do("GET", "/audit-events", nil); r.status != 200 {
			t.Fatal("director cannot read the audit", r.status)
		}
		for _, path := range []string{"/review/claim", "/review/validate", "/review/reject", "/review/reassign", "/review/revert", "/corrections"} {
			if r := director.do("POST", "/applications/"+id+path, map[string]any{"version": v}); r.status != 403 || r.code() != "ADMIN_FORBIDDEN" {
				t.Fatalf("director POST %s: %d", path, r.status)
			}
		}
		if r := deputy.do("POST", "/applications/"+id+"/corrections", map[string]any{"version": v}); r.status != 403 {
			t.Fatal("deputy director proposes")
		}
		if r := deputy.do("POST", "/applications/"+id+"/review/claim", map[string]any{"version": v}); r.status != 403 {
			t.Fatal("deputy director claims")
		}
		for _, a := range []*actor{agentA, lead} {
			if r := a.do("GET", "/corrections", nil); r.status != 403 {
				t.Fatal("agent reads the approval queue")
			}
		}
		for _, path := range []string{"/review/revert", "/review/reassign"} {
			if r := agentA.do("POST", "/applications/"+id+path, map[string]any{"version": v}); r.status != 403 {
				t.Fatal("agent", path)
			}
		}
		if r := agentA.do("POST", "/applications/"+id+"/review/claim", map[string]any{"version": v}); r.status != 200 || r.body["status"] != StatusInReview {
			t.Fatal("claim", r.raw)
		}
		v++
		for _, path := range []string{"/review/validate", "/review/reject"} {
			if r := agentB.do("POST", "/applications/"+id+path, map[string]any{"version": v, "reasonCode": "OTHER", "comment": "x"}); r.status != 403 || r.code() != "REVIEW_NOT_ASSIGNEE" {
				t.Fatalf("agent B %s on A's dossier: %d %s", path, r.status, r.raw)
			}
		}
		if r := agentB.do("POST", "/applications/"+id+"/corrections", map[string]any{"version": v, "subjectId": "00000000-0000-4000-8000-000000000000", "field": "GRADE", "value": "12", "reason": "x"}); r.status != 404 && r.code() != "REVIEW_NOT_ASSIGNEE" {
			t.Fatal("agent B proposes on A's dossier", r.raw)
		}
		if r := agentA.do("POST", "/applications/"+id+"/review/validate", map[string]any{"version": v - 1}); r.status != 409 || r.code() != "REVIEW_VERSION_CONFLICT" {
			t.Fatal("stale version accepted", r.raw)
		}
		if r := agentA.do("POST", "/applications/"+id+"/review/reject", map[string]any{"version": v, "reasonCode": "UNKNOWN", "comment": ""}); r.status != 422 {
			t.Fatal("rejection without valid reason", r.raw)
		}
		if r := agentA.do("POST", "/applications/"+id+"/review/reject", map[string]any{"version": v, "reasonCode": "DOCUMENT_ILLEGIBLE", "comment": "Pièce d'identité floue."}); r.status != 200 || r.body["status"] != StatusRejected {
			t.Fatal("rejection", r.raw)
		}
		v++
		if r := lead.do("POST", "/applications/"+id+"/review/revert", map[string]any{"version": v, "comment": ""}); r.status != 422 {
			t.Fatal("revert without reason")
		}
		if r := lead.do("POST", "/applications/"+id+"/review/revert", map[string]any{"version": v, "comment": "Nouvelle pièce reçue."}); r.status != 200 || r.body["status"] != StatusInReview {
			t.Fatal("revert", r.raw)
		}
		v++
		if r := lead.do("POST", "/applications/"+id+"/review/reassign", map[string]any{"version": v, "assignee": agentB.principal.UserID, "comment": "Agent A absent."}); r.status != 200 {
			t.Fatal("reassign", r.raw)
		}
		v++
		if r := agentA.do("POST", "/applications/"+id+"/review/validate", map[string]any{"version": v}); r.code() != "REVIEW_NOT_ASSIGNEE" {
			t.Fatal("previous agent still decides", r.raw)
		}
		if r := agentB.do("POST", "/applications/"+id+"/review/validate", map[string]any{"version": v}); r.status != 200 || r.body["status"] != StatusValidated {
			t.Fatal("new agent validates", r.raw)
		}
		history := e.count(`SELECT count(*) FROM application_review_events WHERE application_id=$1::uuid`, id)
		if history != 6 || e.count(`SELECT count(*) FROM application_review_events WHERE application_id=$1::uuid AND version BETWEEN 1 AND 6`, id) != 6 {
			t.Fatal("history incomplete", history)
		}
		for _, event := range []string{"REVIEW_CLAIMED", "REVIEW_REJECTED", "REVIEW_REVERTED", "REVIEW_REASSIGNED", "REVIEW_VALIDATED"} {
			if e.count(`SELECT count(*) FROM audit_events WHERE event_type=$1 AND target_id=$2::uuid AND actor_type='ADMIN'`, event, id) != 1 {
				t.Fatal("not audited", event)
			}
		}
	})

	t.Run("one role per account, checked in Go and by the database", func(t *testing.T) {
		if _, err := e.admin.SetGrants(ctx, superAdmin.principal, agentA.principal.UserID, []admin.GrantInput{{Role: admin.RoleReviewer}, {Role: admin.RoleReviewLead}}); problemCode(err) != "VALIDATION_FAILED" {
			t.Fatal("two business roles accepted", err)
		}
		for _, role := range []admin.Role{admin.RoleSuperAdmin, admin.RoleReviewLead, admin.RoleDirector, admin.RoleDeputyDirector} {
			if _, err := e.pool.Exec(ctx, `INSERT INTO admin_role_assignments(admin_user_id,role) VALUES($1::uuid,$2)`, agentA.principal.UserID, role); err == nil || !strings.Contains(err.Error(), "single role") {
				t.Fatal("database accepted a second role", role, err)
			}
		}
		if _, err := e.pool.Exec(ctx, `INSERT INTO admin_role_assignments(admin_user_id,role) VALUES($1::uuid,'AUDITOR')`, director.principal.UserID); err == nil {
			t.Fatal("retired role assigned")
		}
	})

	t.Run("concurrent claims and reassignments: one wins, none overwrites", func(t *testing.T) {
		id, v := e.firstDossier(`r.status='TO_REVIEW'`)
		agents := []*actor{agentA, agentB, lead}
		errs := make([]error, 9)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[i] = e.svc.Transition(ctx, agents[i%3].principal, id, "claim", Action{Version: v})
			}()
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if problemCode(err) != "REVIEW_VERSION_CONFLICT" {
				t.Fatal(err)
			}
		}
		if ok != 1 || version(e, id) != v+1 || e.count(`SELECT count(*) FROM application_review_events WHERE application_id=$1::uuid AND event_type='CLAIMED'`, id) != 1 {
			t.Fatal("concurrent claims", ok)
		}
		secondLead := e.actor("Responsable SE bis", admin.RoleReviewLead)
		v = version(e, id)
		var holder string
		e.pool.QueryRow(ctx, `SELECT assignee_id::text FROM application_reviews WHERE application_id=$1::uuid`, id).Scan(&holder)
		target := agentA.principal.UserID
		if holder == target {
			target = agentB.principal.UserID
		}
		results := make([]error, 2)
		start = make(chan struct{})
		for i, l := range []*actor{lead, secondLead} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, results[i] = e.svc.Transition(ctx, l.principal, id, "reassign", Action{Version: v, Assignee: target, Comment: "Répartition de la charge"})
			}()
		}
		close(start)
		wg.Wait()
		if (results[0] == nil) == (results[1] == nil) || (problemCode(results[0]) != "REVIEW_VERSION_CONFLICT" && problemCode(results[1]) != "REVIEW_VERSION_CONFLICT") {
			t.Fatal("concurrent reassignments", results)
		}
	})

	t.Run("corrections: proposal, blocked validation, four eyes, exact effective average", func(t *testing.T) {
		id, v := e.firstDossier(`r.status='TO_REVIEW'`)
		if _, err := e.svc.Transition(ctx, agentA.principal, id, "claim", Action{Version: v}); err != nil {
			t.Fatal(err)
		}
		v++
		before, _ := e.svc.Detail(ctx, agentA.principal, id)
		v = before.Version
		subject := before.Subjects[0]
		var snapshotAverage string
		e.pool.QueryRow(ctx, `SELECT average::text FROM application_submissions WHERE application_id=$1::uuid`, id).Scan(&snapshotAverage)
		if _, err := e.svc.Propose(ctx, agentB.principal, id, Proposal{Version: v, SubjectID: subject.SubjectID, Field: "GRADE", Value: "13", Reason: "x"}); problemCode(err) != "REVIEW_NOT_ASSIGNEE" {
			t.Fatal("other agent proposed", err)
		}
		if _, err := e.svc.Propose(ctx, agentA.principal, id, Proposal{Version: v, SubjectID: subject.SubjectID, Field: "GRADE", Value: "21", Reason: "Lecture"}); problemCode(err) != "VALIDATION_FAILED" {
			t.Fatal("grade above 20 accepted", err)
		}
		// 10/3 has no finite decimal expansion: the effective average stays an exact fraction.
		d, err := e.svc.Propose(ctx, agentA.principal, id, Proposal{Version: v, SubjectID: subject.SubjectID, Field: "GRADE", Value: "10,333", Reason: "Note mal lue sur le relevé."})
		if err != nil {
			t.Fatal(err)
		}
		v = d.Version
		if _, err := e.svc.Transition(ctx, agentA.principal, id, "validate", Action{Version: v}); problemCode(err) != "REVIEW_CORRECTION_PENDING" {
			t.Fatal("validation with a pending correction", err)
		}
		if _, err := e.pool.Exec(ctx, `UPDATE application_reviews SET status='VALIDATED',version=version+1 WHERE application_id=$1::uuid`, id); err == nil {
			t.Fatal("database validated with a pending correction")
		}
		correction := d.Corrections[0].ID
		if _, err := e.svc.Decide(ctx, lead.principal, correction, true, Decision{Version: v}); err == nil {
			t.Fatal("lead approved") // permission is checked by the route; the service keeps the scope check
		}
		// Four eyes beyond roles: the author can never decide, even holding the approver role.
		author := agentA.principal
		author.Grants = []admin.Grant{{Role: admin.RoleDeputyDirector}}
		if _, err := e.svc.Decide(ctx, author, correction, true, Decision{Version: v}); problemCode(err) != "CORRECTION_FOUR_EYES" {
			t.Fatal("author approved own correction", err)
		}
		if _, err := e.pool.Exec(ctx, `UPDATE application_corrections SET status='APPROVED',decided_by=proposed_by,decided_at=now() WHERE id=$1::uuid`, correction); err == nil {
			t.Fatal("database accepted a self-approval")
		}
		if _, err := e.svc.Decide(ctx, deputy.principal, correction, false, Decision{Version: v}); problemCode(err) != "VALIDATION_FAILED" {
			t.Fatal("refusal without reason", err)
		}
		approved, err := e.svc.Decide(ctx, deputy.principal, correction, true, Decision{Version: v})
		if err != nil {
			t.Fatal(err)
		}
		v = approved.Version
		grades := []submissions.Grade{}
		for _, s := range approved.Subjects {
			g, _ := submissions.ParseNumeric(s.EffectiveGrade)
			c, _ := submissions.ParseNumeric(s.EffectiveCoefficient)
			grades = append(grades, submissions.Grade{Value: g, Coefficient: c})
		}
		want, _ := submissions.ComputeAverage(grades)
		if approved.Subjects[0].EffectiveGrade != "10.333" || approved.Subjects[0].Grade != subject.Grade || approved.EffectiveAverage.Decimal != want.Decimal || approved.EffectiveAverage.Exact != want.Exact {
			t.Fatalf("effective average %+v want %s", approved.EffectiveAverage, want.Decimal)
		}
		expected := new(big.Rat).Quo(want.WeightedSum, want.CoefficientSum)
		weighted, _ := submissions.ParseNumeric(approved.EffectiveAverage.WeightedSum)
		coefficients, _ := submissions.ParseNumeric(approved.EffectiveAverage.CoefficientSum)
		if new(big.Rat).Quo(weighted, coefficients).Cmp(expected) != 0 {
			t.Fatal("stored fraction differs")
		}
		var snapshotNow string
		e.pool.QueryRow(ctx, `SELECT average::text FROM application_submissions WHERE application_id=$1::uuid`, id).Scan(&snapshotNow)
		if snapshotNow != snapshotAverage || approved.SnapshotAverage.Decimal != snapshotAverage {
			t.Fatal("submission snapshot changed")
		}
		if _, err := e.pool.Exec(ctx, `UPDATE application_corrections SET reason='autre' WHERE id=$1::uuid`, correction); err == nil {
			t.Fatal("decided correction edited")
		}
		// A refused coefficient correction changes nothing; then validation is possible.
		d, err = e.svc.Propose(ctx, agentA.principal, id, Proposal{Version: v, SubjectID: subject.SubjectID, Field: "COEFFICIENT", Value: "5", Reason: "Coefficient douteux."})
		if err != nil {
			t.Fatal(err)
		}
		refused, err := e.svc.Decide(ctx, deputy.principal, d.Corrections[1].ID, false, Decision{Version: d.Version, Reason: "Le relevé indique bien la valeur lue."})
		if err != nil || refused.EffectiveAverage.Decimal != want.Decimal {
			t.Fatal("refusal changed the average", err)
		}
		validated, err := e.svc.Transition(ctx, agentA.principal, id, "validate", Action{Version: refused.Version})
		if err != nil || validated.Status != StatusValidated {
			t.Fatal(err)
		}
		queue, _ := e.svc.PendingCorrections(ctx, deputy.principal)
		if slices.ContainsFunc(queue, func(c CorrectionView) bool { return c.ApplicationID == id }) {
			t.Fatal("decided corrections still queued")
		}
		for _, event := range []string{"CORRECTION_PROPOSED", "CORRECTION_APPROVED", "CORRECTION_REFUSED"} {
			if e.count(`SELECT count(*) FROM audit_events WHERE event_type=$1 AND target_id=$2::uuid`, event, id) == 0 {
				t.Fatal("not audited", event)
			}
		}
	})

	t.Run("dossier: duplicates both ways, consultations audited, documents and proofs", func(t *testing.T) {
		var first, second string
		if err := e.pool.QueryRow(ctx, `SELECT related_application_id::text,application_id::text FROM application_submission_flags WHERE code='DUPLICATE_EMAIL' LIMIT 1`).Scan(&first, &second); err != nil {
			t.Fatal(err)
		}
		d1, _ := e.svc.Detail(ctx, director.principal, first)
		d2, _ := e.svc.Detail(ctx, director.principal, second)
		if !slices.ContainsFunc(d1.Flags, func(f FlagView) bool {
			return f.Code == "DUPLICATE_EMAIL" && f.Direction == "INCOMING" && f.ApplicationID == second
		}) ||
			!slices.ContainsFunc(d2.Flags, func(f FlagView) bool {
				return f.Code == "DUPLICATE_EMAIL" && f.Direction == "OUTGOING" && f.ApplicationID == first
			}) {
			t.Fatal("duplicate not visible in both directions", d1.Flags, d2.Flags)
		}
		if d1.Email == "" || d1.Phone == "" {
			t.Fatal("contact details missing from the dossier")
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_DATA_ACCESSED' AND target_id=$1::uuid AND metadata->>'purpose'='REVIEW_DETAIL' AND actor_id=$2::uuid`, first, director.principal.UserID) != 1 {
			t.Fatal("dossier opening not audited")
		}
		// Documents: a stored PDF streams with its own headers; consultation audited.
		var key string
		e.pool.QueryRow(ctx, `SELECT v.storage_key FROM application_submission_documents sd JOIN application_submissions s ON s.id=sd.submission_id
		 JOIN document_versions v ON v.id=sd.document_version_id WHERE s.application_id=$1::uuid AND sd.document_type='TRANSCRIPT'`, first).Scan(&key)
		e.objects.PutContent(ctx, key, bytes.NewReader(make([]byte, 1000)), 1000, "application/pdf")
		r := director.do("GET", "/applications/"+first+"/documents/TRANSCRIPT/content", nil)
		if r.status != 200 || r.header.Get("Content-Type") != "application/pdf" || r.header.Get("X-Content-Type-Options") != "nosniff" ||
			r.header.Get("Cache-Control") != "no-store" || r.header.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'self'" ||
			r.header.Get("X-Frame-Options") != "SAMEORIGIN" || !strings.HasPrefix(r.header.Get("Content-Disposition"), "inline; filename=document-synthetique.pdf") {
			t.Fatal("PDF preview headers", r.status, r.header)
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_DATA_ACCESSED' AND target_id=$1::uuid AND metadata->>'purpose'='DOCUMENT:TRANSCRIPT'`, first) != 1 {
			t.Fatal("document consultation not audited")
		}
		if r := director.do("GET", "/applications/"+first+"/documents/REQUEST_LETTER/content", nil); r.status != 503 {
			t.Fatal("missing object", r.status)
		}
		// Proof: request → job on the dedicated stream → worker routes → image → audited view.
		subject := d1.Subjects[0].SubjectID
		if r := director.do("GET", "/applications/"+first+"/subjects/"+subject+"/proof/image", nil); r.code() != "PROOF_NOT_READY" {
			t.Fatal("image before generation", r.raw)
		}
		if r := director.do("POST", "/applications/"+first+"/subjects/"+subject+"/proof", map[string]any{}); r.status != 202 || r.body["status"] != "PENDING" {
			t.Fatal("proof request", r.raw)
		}
		entries, _ := e.cache.XRange(ctx, e.svc.stream(), "-", "+").Result()
		if len(entries) != 1 {
			t.Fatal("proof not queued on its own stream", len(entries))
		}
		var job proofJob
		json.Unmarshal([]byte(entries[0].Values["job"].(string)), &job)
		var analysisVersion string
		e.pool.QueryRow(ctx, `SELECT o.document_version_id::text FROM application_submissions s JOIN ocr_analyses o ON o.id=s.ocr_analysis_id WHERE s.application_id=$1::uuid`, first).Scan(&analysisVersion)
		if job.DocumentVersion != analysisVersion || job.Page != 1 || !strings.Contains(string(job.Evidence), `"grade"`) {
			t.Fatal("proof job not bound to the analysed version and stored boxes", job)
		}
		internal := func(method, path, contentType string, body []byte, auth bool) int {
			req := httptest.NewRequest(method, path, bytes.NewReader(body))
			if auth {
				req.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 40))
			}
			req.Header.Set("Content-Type", contentType)
			w := httptest.NewRecorder()
			e.internal.ServeHTTP(w, req)
			return w.Code
		}
		if internal("GET", "/internal/proofs/"+job.ProofID+"/document", "", nil, false) != 401 || internal("GET", "/internal/proofs/"+job.ProofID+"/document", "", nil, true) != 200 {
			t.Fatal("internal document route")
		}
		if internal("POST", "/internal/proofs/"+job.ProofID+"/image", "image/png", []byte("not a png"), true) != 422 {
			t.Fatal("non-PNG accepted")
		}
		// The refused image failed the proof: a new request queues it again.
		director.do("POST", "/applications/"+first+"/subjects/"+subject+"/proof", map[string]any{})
		img := image.NewRGBA(image.Rect(0, 0, 40, 12))
		img.Set(1, 1, color.Black)
		var buf bytes.Buffer
		png.Encode(&buf, img)
		if code := internal("POST", "/internal/proofs/"+job.ProofID+"/image", "image/png", buf.Bytes(), true); code != 204 {
			t.Fatal("proof upload", code)
		}
		if r := director.do("GET", "/applications/"+first+"/subjects/"+subject+"/proof", nil); r.body["status"] != "READY" {
			t.Fatal(r.raw)
		}
		shown := director.do("GET", "/applications/"+first+"/subjects/"+subject+"/proof/image", nil)
		if shown.status != 200 || shown.header.Get("Content-Type") != "image/png" || !strings.HasPrefix(shown.raw, "\x89PNG") || !strings.Contains(shown.header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("proof image", shown.status)
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_DATA_ACCESSED' AND target_id=$1::uuid AND metadata->>'purpose'=$2`, first, "OCR_PROOF:"+subject) != 1 {
			t.Fatal("proof consultation not audited")
		}
		var storedKey string
		e.pool.QueryRow(ctx, `SELECT storage_key FROM ocr_proof_images WHERE id=$1::uuid`, job.ProofID).Scan(&storedKey)
		if !strings.HasPrefix(storedKey, "applications/"+first+"/proofs/") {
			t.Fatal("proof stored outside the private application prefix", storedKey)
		}
	})

	t.Run("every admin route keeps frame-ancestors 'none' except the PDF preview", func(t *testing.T) {
		id, _ := e.firstDossier(`true`)
		for _, route := range e.router.Routes() {
			path := strings.TrimPrefix(route.Path, admin.BasePath)
			path = strings.NewReplacer("{id}", id, "{type}", "TRANSCRIPT", "{subject}", "00000000-0000-4000-8000-000000000000").Replace(path)
			for _, a := range []*actor{director, {e: e}} {
				r := a.do(route.Method, path, map[string]any{})
				csp := r.header.Get("Content-Security-Policy")
				pdf := strings.HasSuffix(route.Path, "/content") && r.status == 200
				if !pdf && (!strings.Contains(csp, "frame-ancestors 'none'") || r.header.Get("X-Frame-Options") != "DENY") {
					t.Fatalf("%s %s (%d): %q", route.Method, route.Path, r.status, csp)
				}
			}
		}
	})

	t.Run("ranking freeze blocks every review change", func(t *testing.T) {
		id, v := e.firstDossier(`r.status='TO_REVIEW'`)
		// Since migration 00009 a freeze requires its preconditions (all dossiers decided, campaign
		// closed): to test the phase 8 lock alone, the freeze guards are set aside for this row.
		bypass := func(statement string, args ...any) {
			t.Helper()
			for _, s := range []string{`ALTER TABLE ranking_freezes DISABLE TRIGGER USER`, statement, `ALTER TABLE ranking_freezes ENABLE TRIGGER USER`} {
				var with []any
				if s == statement {
					with = args
				}
				if _, err := e.pool.Exec(ctx, s, with...); err != nil {
					t.Fatal(err)
				}
			}
		}
		bypass(`INSERT INTO ranking_freezes(campaign_program_id,frozen_by,available_seats,ranked_count,not_ranked_count,admitted_count,calculation_version)
		 SELECT campaign_program_id,$2::uuid,1,0,0,0,1 FROM application_reviews WHERE application_id=$1::uuid`, id, director.principal.UserID)
		t.Cleanup(func() { bypass(`DELETE FROM ranking_freezes`) })
		if _, err := e.svc.Transition(ctx, agentA.principal, id, "claim", Action{Version: v}); problemCode(err) != "REVIEW_FROZEN" {
			t.Fatal("claim after freeze", err)
		}
		if _, err := e.pool.Exec(ctx, `UPDATE application_reviews SET status='IN_REVIEW',assignee_id=$2::uuid,version=version+1 WHERE application_id=$1::uuid`, id, agentA.principal.UserID); err == nil || !strings.Contains(err.Error(), "frozen") {
			t.Fatal("database accepted a change after the freeze", err)
		}
		if d, _ := e.svc.Detail(ctx, director.principal, id); !d.Frozen {
			t.Fatal("freeze not shown")
		}
	})

	t.Run("history and decisions are append-only", func(t *testing.T) {
		for _, statement := range []string{`DELETE FROM application_review_events`, `UPDATE application_review_events SET comment='x'`, `DELETE FROM application_reviews`, `DELETE FROM application_corrections`, `TRUNCATE application_review_events`} {
			if _, err := e.pool.Exec(ctx, statement); err == nil {
				t.Fatal("accepted:", statement)
			}
		}
		// 00009 (no ranking yet) rolls back; 00008 refuses with review history.
		if _, err := e.provider.DownTo(ctx, 7); err == nil || !strings.Contains(err.Error(), "Review history exists") {
			t.Fatal("migration 00008 rollback accepted with review history", err)
		}
		if _, err := e.provider.Up(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dashboards: counters for readers, missing directors for the super-admin", func(t *testing.T) {
		d, err := e.svc.Dashboard(ctx, agentA.principal, e.admin)
		if err != nil || len(d.Counters) == 0 || d.Unassigned == nil || d.PendingCorrections == nil || d.Mine == nil || d.Coverage != nil {
			t.Fatal("agent dashboard", d, err)
		}
		total := 0
		for _, c := range d.Counters {
			total += c.Count
		}
		if total != 5000 {
			t.Fatal("counters", total)
		}
		s, err := e.svc.Dashboard(ctx, superAdmin.principal, e.admin)
		if err != nil || s.Counters != nil || s.Coverage == nil || !s.Coverage.Director || !s.Coverage.DeputyDirector {
			t.Fatal("super-admin dashboard", s, err)
		}
		e.pool.Exec(ctx, `UPDATE admin_users SET status='DISABLED',disabled_at=now() WHERE id=$1::uuid`, director.principal.UserID)
		if s, _ = e.svc.Dashboard(ctx, superAdmin.principal, e.admin); s.Coverage.Director {
			t.Fatal("missing director not reported")
		}
	})
}

func TestPostgresReviewMigrationRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e := setup(t, ctx)
	if _, err := GenerateSynthetic(ctx, e.pool, Synthetic{Campaign: "DEV_LOCAL", Count: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.provider.DownTo(ctx, 7); err != nil {
		t.Fatal("rollback with only creation events", err)
	}
	if e.count(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('application_reviews','application_corrections','ocr_proof_images')`) != 0 {
		t.Fatal("rollback incomplete")
	}
	if _, err := e.provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT count(*) FROM application_reviews WHERE status='TO_REVIEW'`) != 20 {
		t.Fatal("existing submissions not backfilled")
	}
}
