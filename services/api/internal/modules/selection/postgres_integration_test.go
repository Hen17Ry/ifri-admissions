//go:build integration

package selection

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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"ifri-admissions/services/api/internal/modules/review"
	"ifri-admissions/services/api/internal/platform/passwords"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
	"ifri-admissions/services/api/internal/platform/storage"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

const origin = "http://localhost:5173"

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
	reviews  *review.Service
	admin    *admin.Service
	mux      *http.ServeMux
	internal *http.ServeMux
	cache    *redis.Client
	stream   string
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
	schema := "ifri_selection_test_" + hex.EncodeToString(suffix)
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE") })
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pc.MaxConns = 12
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
	stream := "ifri:test:exports:" + schema
	t.Cleanup(func() {
		keys, _ := cache.Keys(context.Background(), "ifri:test:"+schema+":*").Result()
		cache.Del(context.Background(), append(keys, stream, "ifri:test:proofs:"+schema)...)
		cache.Close()
	})
	keys, _ := secrets.Derive(bytes.Repeat([]byte{7}, 32), 1)
	hasher, _ := passwords.NewHasher(passwords.Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltBytes: 16, KeyBytes: 32}, 2)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSvc := &admin.Service{Pool: pool, Keys: keys, Hasher: hasher, Limiter: ratelimit.Redis{Client: cache, Prefix: "ifri:test:" + schema + ":"}, Logger: quiet,
		MFARequired: true, Idle: 30 * time.Minute, Absolute: 8 * time.Hour, ActivationTTL: time.Hour, Origin: origin}
	objects := &memoryObjects{objects: map[string][]byte{}, types: map[string]string{}}
	reviews := &review.Service{Pool: pool, Objects: objects, Cache: cache, Logger: quiet, Stream: "ifri:test:proofs:" + schema}
	svc := &Service{Pool: pool, Objects: objects, Cache: cache, Logger: quiet, Stream: stream}
	mux := http.NewServeMux()
	router := admin.Register(mux, adminSvc, origin, false, quiet)
	review.Register(router, reviews)
	Register(router, svc)
	internal := http.NewServeMux()
	svc.RegisterInternal(internal, strings.Repeat("s", 40))
	return &env{t: t, ctx: ctx, pool: pool, provider: provider, svc: svc, reviews: reviews, admin: adminSvc, mux: mux, internal: internal, cache: cache, stream: stream}
}

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
	r.AddCookie(&http.Cookie{Name: admin.CookieName, Value: a.token})
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

func (e *env) offer(program string) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(e.ctx, `SELECT cp.id::text FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id JOIN admission_campaigns c ON c.id=cp.campaign_id
	 WHERE c.code='DEV_LOCAL' AND p.code=$1`, program).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// dossier finds a synthetic dossier by its (unique) last name.
func (e *env) dossier(lastName string) string {
	e.t.Helper()
	var id string
	if err := e.pool.QueryRow(e.ctx, `SELECT application_id::text FROM application_submissions WHERE last_name=$1`, lastName).Scan(&id); err != nil {
		e.t.Fatal(lastName, err)
	}
	return id
}

func (e *env) reviewVersion(id string) int {
	return e.count(`SELECT version FROM application_reviews WHERE application_id=$1::uuid`, id)
}

// decide claims a dossier and validates (or rejects) it as the agent.
func (e *env) decide(agent *actor, id string, validate bool) {
	e.t.Helper()
	v := e.reviewVersion(id)
	if _, err := e.reviews.Transition(e.ctx, agent.principal, id, "claim", review.Action{Version: v}); err != nil {
		e.t.Fatal("claim", err)
	}
	action := review.Action{Version: v + 1}
	name := "validate"
	if !validate {
		name, action.ReasonCode, action.Comment = "reject", "OTHER", "Dossier de test rejeté."
	}
	if _, err := e.reviews.Transition(e.ctx, agent.principal, id, name, action); err != nil {
		e.t.Fatal(name, err)
	}
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

func generate(t *testing.T, e *env, program, qualification string, names []string, grades, coefficients [][]string, at []time.Time) {
	t.Helper()
	if _, err := review.GenerateSynthetic(e.ctx, e.pool, review.Synthetic{Campaign: "DEV_LOCAL", Count: len(names), Programs: []string{program},
		Qualifications: []string{qualification}, LastNames: names, Grades: grades, Coefficients: coefficients, SubmittedAt: at}); err != nil {
		t.Fatal(program, qualification, err)
	}
}

var start = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

func hours(h float64) time.Time { return start.Add(time.Duration(h * float64(time.Hour))) }

func ones(n int) [][]string {
	out := make([][]string, n)
	for i := range out {
		out[i] = []string{"1", "1", "1"}
	}
	return out
}

func entryNames(v View) []string {
	out := []string{}
	for _, e := range v.Entries {
		out = append(out, e.LastName)
	}
	return out
}

func byName(v View, name string) Entry {
	for _, e := range v.Entries {
		if e.LastName == name {
			return e
		}
	}
	return Entry{}
}

func TestPostgresSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	e := setup(t, ctx)
	// SEIOT: one tie of exactly equal fractions decided by mathematics (40/3 = 80/6), one tie
	// group with a DT/EAP candidate (no mathematics subject: criterion skipped, date decides),
	// one tie decided by the number (same date), one candidate below the minimum average, one
	// rejected dossier, and a last name starting with a dash for the CSV formula guard.
	generate(t, e, "SEIOT", "BAC_D", []string{"Alpha", "Bravo", "Charlie", "-Tiret", "Romeo"},
		[][]string{{"16", "14", "12"}, {"12", "15", "15"}, {"10", "10", "10"}, {"15", "15", "15"}, {"18", "18", "18"}}, ones(5),
		[]time.Time{hours(1), hours(2), hours(3), hours(4), hours(4.5)})
	generate(t, e, "SEIOT", "DT_EAP", []string{"Delta"}, [][]string{{"14", "14", "14"}}, ones(1), []time.Time{hours(0.5)})
	generate(t, e, "SEIOT", "BAC_C", []string{"Echo", "Foxtrot"}, [][]string{{"13", "13", "13"}, {"13", "13", "13"}}, ones(2), []time.Time{hours(5), hours(5)})
	generate(t, e, "SEIOT", "BAC_E", []string{"Golf", "Hotel"}, [][]string{{"12", "14", "14"}, {"13", "13", "14"}}, [][]string{{"1", "1", "1"}, {"2", "2", "2"}},
		[]time.Time{hours(6), hours(7)})
	generate(t, e, "IA", "BAC_D", []string{"India", "Juliet", "Kilo"}, nil, nil, nil)
	seiot, ia := e.offer("SEIOT"), e.offer("IA")
	superAdmin := e.actor("Super Admin", admin.RoleSuperAdmin)
	agent := e.actor("Agent Un", admin.RoleReviewer)
	lead := e.actor("Responsable", admin.RoleReviewLead)
	deputy := e.actor("Directeur Adjoint", admin.RoleDeputyDirector)
	director := e.actor("Directeur", admin.RoleDirector)
	seats := func(n int) *int { return &n }

	t.Run("provisional ranking: validated dossiers only, computed on demand, blockers listed", func(t *testing.T) {
		v, err := e.svc.View(ctx, director.principal, seiot)
		if err != nil || len(v.Entries) != 0 || v.Freeze != nil || v.Counts.ToReview != 10 {
			t.Fatal(v.Counts, err)
		}
		codes := []string{}
		for _, b := range v.Blockers {
			codes = append(codes, b.Code)
		}
		if !slices.Equal(codes, []string{"CAMPAIGN_OPEN", "DOSSIERS_PENDING", "SEATS_MISSING"}) {
			t.Fatal(codes)
		}
		for _, name := range []string{"Alpha", "Bravo", "Charlie", "-Tiret", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"} {
			e.decide(agent, e.dossier(name), true)
		}
		e.decide(agent, e.dossier("Romeo"), false)
		v, _ = e.svc.View(ctx, lead.principal, seiot)
		if len(v.Entries) != 9 || v.Entries[0].Decision != "" || byName(v, "Romeo").Number != "" {
			t.Fatal("provisional ranking", entryNames(v))
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_DATA_ACCESSED' AND target_id=$1::uuid AND metadata->>'purpose'='RANKING'`, seiot) != 2 {
			t.Fatal("ranking consultations not audited")
		}
	})

	t.Run("roles × selection routes over HTTP", func(t *testing.T) {
		readers := map[*actor]bool{director: true, deputy: true, lead: true, agent: false, superAdmin: false}
		for a, allowed := range readers {
			for _, path := range []string{"/selection/offers", "/selection/offers/" + seiot} {
				if r := a.do("GET", path, nil); (r.status == 200) != allowed || (!allowed && r.code() != "ADMIN_FORBIDDEN") {
					t.Fatal(a.principal.DisplayName, path, r.status)
				}
			}
		}
		routes := []struct {
			method, path string
			allowed      *actor
			also         *actor
		}{
			{"PUT", "/selection/offers/" + seiot + "/settings", director, nil},
			{"POST", "/selection/offers/" + seiot + "/freeze", director, nil},
			{"POST", "/selection/offers/" + seiot + "/withdrawals", deputy, nil},
			{"POST", "/selection/offers/" + seiot + "/promotions", deputy, nil},
			{"GET", "/selection/offers/" + seiot + "/export.csv?variant=public", director, deputy},
			{"POST", "/selection/offers/" + seiot + "/exports", director, deputy},
		}
		for _, route := range routes {
			for _, a := range []*actor{superAdmin, agent, lead, deputy, director} {
				if a == route.allowed || a == route.also {
					continue
				}
				if r := a.do(route.method, route.path, map[string]any{}); r.status != 403 || r.code() != "ADMIN_FORBIDDEN" {
					t.Fatal(a.principal.DisplayName, route.method, route.path, r.status)
				}
			}
		}
	})

	t.Run("seats and minimum average: bounds, until the freeze, audited", func(t *testing.T) {
		for _, bad := range []map[string]any{{"seats": 0}, {"seats": 10001}, {"seats": 3, "minimumAverage": "20.01"}, {"seats": 3, "minimumAverage": "10.555"}, {"minimumAverage": "dix"}} {
			if r := director.do("PUT", "/selection/offers/"+seiot+"/settings", bad); r.status != 422 {
				t.Fatal("accepted", bad, r.status)
			}
		}
		for _, good := range []map[string]any{{"seats": 1}, {"seats": 10000, "minimumAverage": "20"}, {"seats": 3, "minimumAverage": "11,00"}} {
			if r := director.do("PUT", "/selection/offers/"+seiot+"/settings", good); r.status != 200 {
				t.Fatal("refused", good, r.raw)
			}
		}
		v, _ := e.svc.View(ctx, director.principal, seiot)
		if *v.Offer.Seats != 3 || v.Offer.MinimumAverage != "11.00" || e.count(`SELECT count(*) FROM audit_events WHERE event_type='RANKING_SETTINGS_CHANGED' AND target_id=$1::uuid`, seiot) != 3 {
			t.Fatal("settings", v.Offer, e.count(`SELECT count(*) FROM audit_events WHERE event_type='RANKING_SETTINGS_CHANGED'`))
		}
		// The quota line: three admitted, then the waiting list, then the candidate below 11.
		decisions := map[string]string{}
		for _, entry := range v.Entries {
			decisions[entry.LastName] = entry.Decision
		}
		if decisions["Alpha"] != Admitted || decisions["Bravo"] != Waitlisted || decisions["Charlie"] != NotRanked {
			t.Fatal(decisions)
		}
	})

	t.Run("every precondition of the freeze, in the transaction and in the database", func(t *testing.T) {
		freeze := func(s Settings) error { _, err := e.svc.Freeze(ctx, director.principal, seiot, s); return err }
		three := Settings{Seats: seats(3), MinimumAverage: "11"}
		// Campaign still open.
		if err := freeze(three); problemCode(err) != "RANKING_FREEZE_BLOCKED" || !strings.Contains(fmt.Sprint(err.(*admin.Problem).Fields), "CAMPAIGN_OPEN") {
			t.Fatal("open campaign", err)
		}
		if _, err := e.pool.Exec(ctx, `INSERT INTO ranking_freezes(campaign_program_id,frozen_by,available_seats,minimum_average,ranked_count,not_ranked_count,admitted_count,calculation_version)
		 VALUES($1::uuid,$2::uuid,3,11,8,1,3,1)`, seiot, director.principal.UserID); err == nil || !strings.Contains(err.Error(), "application period") {
			t.Fatal("database froze an open campaign", err)
		}
		e.pool.Exec(ctx, `UPDATE admission_campaigns SET status='CLOSED' WHERE code='DEV_LOCAL'`)
		// The settings shown to the Director must be the ones applied.
		if err := freeze(Settings{Seats: seats(4), MinimumAverage: "11"}); problemCode(err) != "RANKING_SETTINGS_CHANGED" {
			t.Fatal("stale settings", err)
		}
		// A dossier back in review, then a pending correction.
		alpha := e.dossier("Alpha")
		if _, err := e.reviews.Transition(ctx, lead.principal, alpha, "revert", review.Action{Version: e.reviewVersion(alpha), Comment: "Pièce à revoir."}); err != nil {
			t.Fatal(err)
		}
		if err := freeze(three); problemCode(err) != "RANKING_FREEZE_BLOCKED" || !strings.Contains(fmt.Sprint(err.(*admin.Problem).Fields), "DOSSIERS_PENDING") {
			t.Fatal("pending dossier", err)
		}
		var subject string
		e.pool.QueryRow(ctx, `SELECT x.subject_id::text FROM application_submission_subjects x JOIN application_submissions s ON s.id=x.submission_id
		 WHERE s.application_id=$1::uuid AND x.position=2`, alpha).Scan(&subject)
		if _, err := e.reviews.Propose(ctx, agent.principal, alpha, review.Proposal{Version: e.reviewVersion(alpha), SubjectID: subject, Field: "GRADE", Value: "15", Reason: "Lecture vérifiée."}); err != nil {
			t.Fatal(err)
		}
		if err := freeze(three); problemCode(err) != "RANKING_FREEZE_BLOCKED" || !strings.Contains(fmt.Sprint(err.(*admin.Problem).Fields), "CORRECTIONS_PENDING") {
			t.Fatal("pending correction", err)
		}
		var correction string
		e.pool.QueryRow(ctx, `SELECT id::text FROM application_corrections WHERE application_id=$1::uuid AND status='PENDING'`, alpha).Scan(&correction)
		if _, err := e.reviews.Decide(ctx, deputy.principal, correction, false, review.Decision{Version: e.reviewVersion(alpha), Reason: "La note du relevé est juste."}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.reviews.Transition(ctx, agent.principal, alpha, "validate", review.Action{Version: e.reviewVersion(alpha)}); err != nil {
			t.Fatal(err)
		}
		// Seats unset.
		if r := director.do("PUT", "/selection/offers/"+seiot+"/settings", map[string]any{"seats": nil, "minimumAverage": "11"}); r.status != 200 {
			t.Fatal(r.raw)
		}
		if err := freeze(Settings{MinimumAverage: "11"}); problemCode(err) != "RANKING_FREEZE_BLOCKED" || !strings.Contains(fmt.Sprint(err.(*admin.Problem).Fields), "SEATS_MISSING") {
			t.Fatal("no seats", err)
		}
		director.do("PUT", "/selection/offers/"+seiot+"/settings", map[string]any{"seats": 3, "minimumAverage": "11"})
		if r := director.do("POST", "/selection/offers/"+seiot+"/freeze", map[string]any{"seats": 3, "minimumAverage": "11"}); r.status != 200 {
			t.Fatal("freeze", r.raw)
		}
		if r := director.do("POST", "/selection/offers/"+seiot+"/freeze", map[string]any{"seats": 3, "minimumAverage": "11"}); r.code() != "RANKING_FROZEN" {
			t.Fatal("second freeze", r.raw)
		}
	})

	t.Run("frozen snapshot: order, decisive criteria, skipped mathematics, decisions", func(t *testing.T) {
		v, err := e.svc.View(ctx, deputy.principal, seiot)
		if err != nil || v.Freeze == nil || v.Freeze.ListVersion != 1 || v.Freeze.Seats != 3 {
			t.Fatal(v.Freeze, err)
		}
		if names := entryNames(v); !slices.Equal(names, []string{"-Tiret", "Delta", "Alpha", "Bravo", "Hotel", "Golf", "Echo", "Foxtrot", "Charlie"}) {
			t.Fatal(names)
		}
		want := []struct{ criterion, math, decision string }{
			{"", MathNotNeeded, Admitted}, {CriterionAverage, MathSkipped, Admitted}, {CriterionDate, MathSkipped, Admitted},
			{CriterionDate, MathSkipped, Waitlisted}, {CriterionAverage, MathCompared, Waitlisted}, {CriterionMath, MathCompared, Waitlisted},
			{CriterionAverage, MathCompared, Waitlisted}, {CriterionNumber, MathCompared, Waitlisted}, {CriterionAverage, MathNotNeeded, NotRanked},
		}
		for i, entry := range v.Entries {
			if entry.DecisiveCriterion != want[i].criterion || entry.MathTiebreak != want[i].math || entry.InitialDecision != want[i].decision || entry.Decision != want[i].decision {
				t.Fatalf("%d %s: %+v", i, entry.LastName, entry)
			}
		}
		if byName(v, "Charlie").Rank != nil || *byName(v, "Foxtrot").Rank != 8 || byName(v, "Delta").MathGrade != "" || byName(v, "Hotel").Average.Display != "13.33" {
			t.Fatal("ranks and values")
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='RANKING_FROZEN' AND target_id=$1::uuid`, seiot) != 1 {
			t.Fatal("freeze not audited")
		}
	})

	t.Run("after the freeze: settings, reviews, corrections and snapshot are immutable", func(t *testing.T) {
		if r := director.do("PUT", "/selection/offers/"+seiot+"/settings", map[string]any{"seats": 4}); r.code() != "RANKING_FROZEN" {
			t.Fatal(r.raw)
		}
		bravo := e.dossier("Bravo")
		if _, err := e.reviews.Transition(ctx, lead.principal, bravo, "revert", review.Action{Version: e.reviewVersion(bravo), Comment: "x"}); problemCode(err) != "REVIEW_FROZEN" {
			t.Fatal("revert after freeze", err)
		}
		for _, statement := range []string{
			`UPDATE campaign_programs SET available_seats=5 WHERE id='` + seiot + `'`,
			`UPDATE campaign_programs SET minimum_average=NULL WHERE id='` + seiot + `'`,
			`UPDATE ranking_entries SET rank=rank`,
			`DELETE FROM ranking_entries`,
			`UPDATE ranking_freezes SET available_seats=4`,
			`DELETE FROM ranking_freezes`,
			`TRUNCATE ranking_entries`,
		} {
			if _, err := e.pool.Exec(ctx, statement); err == nil {
				t.Fatal("accepted:", statement)
			}
		}
		// A row written after the freeze transaction is refused, even a faithful copy.
		if _, err := e.pool.Exec(ctx, `INSERT INTO ranking_entries(campaign_program_id,application_id,position,rank,weighted_sum,coefficient_sum,average,average_is_exact,
		 average_display,math_tiebreak,submitted_at,application_number,decisive_criterion,initial_decision)
		 SELECT campaign_program_id,application_id,99,NULL,weighted_sum,coefficient_sum,average,average_is_exact,average_display,math_tiebreak,submitted_at,application_number,'AVERAGE','NOT_RANKED'
		 FROM ranking_entries WHERE application_number=(SELECT max(application_number) FROM ranking_entries)`); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatal("late entry accepted", err)
		}
		if _, err := e.provider.Down(ctx); err == nil || !strings.Contains(err.Error(), "rollback refused") {
			t.Fatal("migration 00009 rollback accepted with a frozen ranking", err)
		}
	})

	t.Run("withdrawals and promotions: strictly in order, free seat, versions, multiple events", func(t *testing.T) {
		move := func(kind, name string, version int, reason string) error {
			_, err := e.svc.Record(ctx, deputy.principal, seiot, kind, Move{ApplicationID: e.dossier(name), ListVersion: version, Reason: reason})
			return err
		}
		if err := move("PROMOTED", "Bravo", 1, ""); problemCode(err) != "SELECTION_NO_FREE_SEAT" {
			t.Fatal("promotion without a free seat", err)
		}
		if err := move("WITHDRAWN", "Delta", 1, " "); problemCode(err) != "VALIDATION_FAILED" {
			t.Fatal("withdrawal without reason", err)
		}
		if err := move("WITHDRAWN", "Delta", 1, "Inscrit dans une autre école."); err != nil {
			t.Fatal(err)
		}
		if err := move("WITHDRAWN", "Alpha", 1, "Double clic"); problemCode(err) != "SELECTION_VERSION_CONFLICT" {
			t.Fatal("stale version", err)
		}
		if err := move("PROMOTED", "Hotel", 2, ""); problemCode(err) != "SELECTION_ORDER" {
			t.Fatal("skipped the first waiting candidate", err)
		}
		if err := move("WITHDRAWN", "Alpha", 2, "Ne donne pas suite."); err != nil {
			t.Fatal(err)
		}
		v, _ := e.svc.View(ctx, deputy.principal, seiot)
		if v.Freeze.FreeSeats != 2 || v.Proposal == nil || v.Proposal.LastName != "Bravo" || v.Freeze.ListVersion != 3 {
			t.Fatal("two withdrawals", v.Freeze, v.Proposal)
		}
		for i, name := range []string{"Bravo", "Hotel"} {
			if err := move("PROMOTED", name, 3+i, ""); err != nil {
				t.Fatal(name, err)
			}
		}
		if err := move("WITHDRAWN", "Golf", 5, "Liste d’attente : renonce."); err != nil {
			t.Fatal(err)
		}
		if err := move("PROMOTED", "Echo", 6, ""); problemCode(err) != "SELECTION_NO_FREE_SEAT" {
			t.Fatal(err)
		}
		if err := move("WITHDRAWN", "Bravo", 6, "Renonce après promotion."); err != nil {
			t.Fatal(err)
		}
		if err := move("WITHDRAWN", "Charlie", 7, "Non classé"); problemCode(err) != "SELECTION_STATE_INVALID" {
			t.Fatal("withdrawal of a candidate not ranked", err)
		}
		v, _ = e.svc.View(ctx, deputy.principal, seiot)
		if v.Proposal == nil || v.Proposal.LastName != "Echo" || v.Freeze.ListVersion != 7 || len(v.Events) != 6 {
			t.Fatal("proposal skips the withdrawn waiting candidate", v.Proposal, v.Freeze)
		}
		states := map[string]string{}
		for _, entry := range v.Entries {
			states[entry.LastName] = entry.Decision
		}
		if fmt.Sprint(states) != fmt.Sprint(map[string]string{"-Tiret": Admitted, "Delta": Withdrawn, "Alpha": Withdrawn, "Bravo": Withdrawn, "Hotel": Admitted,
			"Golf": Withdrawn, "Echo": Waitlisted, "Foxtrot": Waitlisted, "Charlie": NotRanked}) {
			t.Fatal(states)
		}
		// The database refuses an out-of-order promotion and a skipped version on its own.
		for _, statement := range []string{
			`INSERT INTO selection_events(campaign_program_id,list_version,event_type,application_id,actor_id) VALUES('` + seiot + `',8,'PROMOTED','` + e.dossier("Foxtrot") + `','` + deputy.principal.UserID + `')`,
			`INSERT INTO selection_events(campaign_program_id,list_version,event_type,application_id,reason,actor_id) VALUES('` + seiot + `',9,'WITHDRAWN','` + e.dossier("Echo") + `','x','` + deputy.principal.UserID + `')`,
			`UPDATE selection_events SET reason='x'`,
			`DELETE FROM selection_events`,
		} {
			if _, err := e.pool.Exec(ctx, statement); err == nil {
				t.Fatal("accepted:", statement)
			}
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='SELECTION_WITHDRAWN' AND target_id=$1::uuid`, seiot) != 4 ||
			e.count(`SELECT count(*) FROM audit_events WHERE event_type='SELECTION_PROMOTED' AND target_id=$1::uuid`, seiot) != 2 {
			t.Fatal("selection events not audited")
		}
	})

	t.Run("exports: public CSV without averages, internal criteria, versioned PDF job, audit", func(t *testing.T) {
		public := deputy.do("GET", "/selection/offers/"+seiot+"/export.csv?variant=public", nil)
		if public.status != 200 || !strings.HasPrefix(public.header.Get("Content-Type"), "text/csv") ||
			!strings.Contains(public.header.Get("Content-Disposition"), "liste-seiot-dev_local-v7-publique.csv") {
			t.Fatal(public.status, public.header)
		}
		text := public.raw
		for _, want := range []string{"Version 7 du ", "(gel du ", "Généré le", "Rang;Numéro;Nom;Prénoms;Décision", ";'-Tiret;", ";Hotel;", ";Echo;", "Liste d’attente"} {
			if !strings.Contains(text, want) {
				t.Fatal("public CSV lacks", want, text)
			}
		}
		for _, forbidden := range []string{"Moyenne", "oyenne", "15,00", "13,33", "13,00", "Delta", "Charlie", "Désisté", "Non classé", "@", "+229", ";-Tiret"} {
			if strings.Contains(text, forbidden) {
				t.Fatal("public CSV contains", forbidden)
			}
		}
		internal := director.do("GET", "/selection/offers/"+seiot+"/export.csv?variant=interne", nil)
		for _, want := range []string{"13,33", "Maths non comparées : candidat sans matière mathématique dans le groupe", "Note de mathématiques", "Numéro de candidature", "Désisté", "Non classé"} {
			if !strings.Contains(internal.raw, want) {
				t.Fatal("internal CSV lacks", want)
			}
		}
		// PDF: the job carries only the export identifier; names travel over the private network.
		requested := deputy.do("POST", "/selection/offers/"+seiot+"/exports", map[string]any{"variant": "public"})
		if requested.status != 202 || requested.body["status"] != "PENDING" || requested.body["listVersion"] != float64(7) {
			t.Fatal(requested.raw)
		}
		id := fmt.Sprint(requested.body["id"])
		messages, err := e.cache.XRange(ctx, e.stream, "-", "+").Result()
		if err != nil || len(messages) != 1 || messages[0].Values["job"] != `{"contractVersion":1,"exportId":"`+id+`"}` {
			t.Fatal("export job", messages, err)
		}
		call := func(method, path, contentType string, body []byte) (int, []byte) {
			r := httptest.NewRequest(method, "/internal/exports/"+id+path, bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 40))
			if contentType != "" {
				r.Header.Set("Content-Type", contentType)
			}
			w := httptest.NewRecorder()
			e.internal.ServeHTTP(w, r)
			return w.Code, w.Body.Bytes()
		}
		status, raw := call("GET", "/payload", "", nil)
		var payload Document
		if status != 200 || json.Unmarshal(raw, &payload) != nil || payload.Variant != VariantPublic || len(payload.Rows) != 4 || strings.Contains(string(raw), "13,33") {
			t.Fatal("payload", status, string(raw))
		}
		if status, _ = call("POST", "/document", "application/pdf", []byte("not a pdf")); status != 422 {
			t.Fatal("invalid PDF accepted", status)
		}
		again := director.do("POST", "/selection/offers/"+seiot+"/exports", map[string]any{"variant": "internal"})
		id = fmt.Sprint(again.body["id"])
		pdf := []byte("%PDF-1.7\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
		if status, _ = call("POST", "/document", "application/pdf", pdf); status != 204 {
			t.Fatal("PDF refused", status)
		}
		if state := director.do("GET", "/selection/exports/"+id, nil); state.body["status"] != "READY" || state.body["variant"] != VariantInternal {
			t.Fatal(state.raw)
		}
		content := director.do("GET", "/selection/exports/"+id+"/content", nil)
		if content.status != 200 || content.header.Get("Content-Type") != "application/pdf" || content.raw != string(pdf) ||
			!strings.Contains(content.header.Get("Content-Disposition"), "liste-seiot-dev_local-v7-interne.pdf") {
			t.Fatal(content.status, content.header)
		}
		if lead.do("GET", "/selection/exports/"+id+"/content", nil).status != 403 {
			t.Fatal("lead downloaded an export")
		}
		if e.count(`SELECT count(*) FROM audit_events WHERE event_type='SELECTION_EXPORTED' AND target_id=$1::uuid`, seiot) != 4 ||
			e.count(`SELECT count(*) FROM audit_events WHERE event_type='ADMIN_DATA_ACCESSED' AND metadata->>'purpose'='SELECTION_EXPORT:INTERNAL'`) != 1 {
			t.Fatal("exports not audited")
		}
	})

	t.Run("a freeze and a revert at the same time: never both", func(t *testing.T) {
		for _, name := range []string{"India", "Juliet", "Kilo"} {
			e.decide(agent, e.dossier(name), true)
		}
		director.do("PUT", "/selection/offers/"+ia+"/settings", map[string]any{"seats": 2})
		kilo := e.dossier("Kilo")
		var wg sync.WaitGroup
		var freezeErr, revertErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, freezeErr = e.svc.Freeze(ctx, director.principal, ia, Settings{Seats: seats(2)})
		}()
		go func() {
			defer wg.Done()
			_, revertErr = e.reviews.Transition(ctx, lead.principal, kilo, "revert", review.Action{Version: e.reviewVersion(kilo), Comment: "Vérification."})
		}()
		wg.Wait()
		frozen := e.count(`SELECT count(*) FROM ranking_freezes WHERE campaign_program_id=$1::uuid`, ia) == 1
		status := ""
		e.pool.QueryRow(ctx, `SELECT status FROM application_reviews WHERE application_id=$1::uuid`, kilo).Scan(&status)
		switch {
		case freezeErr == nil && revertErr == nil:
			t.Fatal("freeze and revert both applied")
		case freezeErr == nil && (!frozen || status != "VALIDATED" || problemCode(revertErr) != "REVIEW_FROZEN"):
			t.Fatal("freeze won but", status, revertErr)
		case revertErr == nil && (frozen || status != "IN_REVIEW" || problemCode(freezeErr) != "RANKING_FREEZE_BLOCKED"):
			t.Fatal("revert won but", frozen, freezeErr)
		}
	})
}

func TestPostgresSelectionMigrationRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e := setup(t, ctx)
	if _, err := e.provider.Down(ctx); err != nil {
		t.Fatal("rollback without ranking", err)
	}
	if e.count(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('ranking_entries','selection_events','selection_exports','ranking_math_subjects')`) != 0 {
		t.Fatal("rollback incomplete")
	}
	if _, err := e.provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE campaign_programs SET minimum_average=10 WHERE id=$1::uuid`, e.offer("GL")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.provider.Down(ctx); err == nil || !strings.Contains(err.Error(), "rollback refused") {
		t.Fatal("rollback erased a minimum average", err)
	}
}
