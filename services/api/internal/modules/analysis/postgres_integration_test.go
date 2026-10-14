//go:build integration

package analysis

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/documents"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPostgresAnalysis(t *testing.T) {
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var id string
	if err = admin.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	schema := "ifri_analysis_" + strings.ReplaceAll(id, "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := stdlib.OpenDB(*pc.ConnConfig)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err = seeds.Development(ctx, pool, "test"); err != nil {
		t.Fatal(err)
	}
	_, hash, _ := drafts.NewSecret()
	draftRepo := drafts.NewRepository(pool)
	d, err := draftRepo.Create(ctx, "DEV_LOCAL", hash, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	year := 2025
	d, err = draftRepo.Update(ctx, hash, "academic-background", drafts.Change{Version: d.Version, Qualification: "DT_DWM", GraduationYear: &year})
	if err != nil {
		t.Fatal(err)
	}
	d, err = draftRepo.Update(ctx, hash, "program", drafts.Change{Version: d.Version, Program: "GL"})
	if err != nil {
		t.Fatal(err)
	}
	docs := documents.NewRepository(pool)
	for _, code := range []string{"REQUEST_LETTER", "TRANSCRIPT", "IDENTITY_DOCUMENT", "PARENTAL_COMMITMENT"} {
		var vid string
		pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&vid)
		err = docs.Save(ctx, hash, d.Version, code, documents.Version{ID: vid, Key: "applications/" + d.ID + "/documents/" + code + "/" + vid + ".pdf", Filename: "synthetic.pdf", SHA256: strings.Repeat("0", 64), Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		d, err = draftRepo.Current(ctx, hash)
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := Repository{pool, 0.85}
	v, err := repo.Current(ctx, hash)
	if err != nil || v.Status != "QUEUED" {
		t.Fatalf("queue: %v %v", v, err)
	}
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM ocr_analyses`).Scan(&count)
	if count != 1 {
		t.Fatal("non transcript triggered analysis")
	}
	// Redis enqueue/consume/replay/ACK on an isolated stream via the dispatcher contract.
	cache := redis.NewClient(&redis.Options{Addr: c.RedisAddress(), Password: c.RedisPassword})
	defer cache.Close()
	testStream := "ifri:test:ocr:" + schema
	defer cache.Del(context.Background(), testStream)
	if err = repo.dispatchToStream(ctx, cache, testStream); err != nil {
		t.Fatal(err)
	}
	entries, err := cache.XRevRangeN(ctx, testStream, "+", "-", 10).Result()
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, entry := range entries {
		var job struct {
			ID string `json:"analysisId"`
		}
		json.Unmarshal([]byte(entry.Values["job"].(string)), &job)
		if job.ID == v.ID {
			matched = true
			defer cache.XDel(context.Background(), testStream, entry.ID)
		}
	}
	if !matched {
		t.Fatal("Redis enqueue missing")
	}
	if err = cache.XGroupCreateMkStream(ctx, testStream, "test-group", "0").Err(); err != nil {
		t.Fatal(err)
	}
	batch, err := cache.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "test-group", Consumer: "before-restart", Streams: []string{testStream, ">"}, Count: 1}).Result()
	if err != nil || len(batch) != 1 || len(batch[0].Messages) != 1 {
		t.Fatal("Redis consume", err)
	}
	replay, _, err := cache.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: testStream, Group: "test-group", Consumer: "after-restart", MinIdle: 0, Start: "0-0", Count: 1}).Result()
	if err != nil || len(replay) != 1 {
		t.Fatal("Redis restart recovery", err)
	}
	if err = cache.XAck(ctx, testStream, "test-group", replay[0].ID).Err(); err != nil {
		t.Fatal(err)
	}
	pending, err := cache.XPending(ctx, testStream, "test-group").Result()
	if err != nil || pending.Count != 0 {
		t.Fatal("Redis ACK", err)
	}
	internal := http.NewServeMux()
	secret := strings.Repeat("s", 40)
	Handler{Repo: repo, Secret: secret}.RegisterInternal(internal)
	claim := httptest.NewRequest("POST", "/internal/ocr/"+v.ID+"/claim", strings.NewReader(`{}`))
	claim.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	internal.ServeHTTP(response, claim)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "PROCESSING") {
		t.Fatal("internal claim", response.Body.String())
	}
	var raw []byte
	pool.QueryRow(ctx, `SELECT expected_subjects FROM ocr_analyses WHERE id=$1::uuid`, v.ID).Scan(&raw)
	var expected []Expected
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	result := Result{ContractVersion: 1, Attempt: 1, Status: "SUCCEEDED", Engine: "synthetic-contract", EngineVersion: "1"}
	for i, e := range expected {
		result.Subjects = append(result.Subjects, subjectOnRow(e, i))
	}
	if err = repo.Complete(ctx, v.ID, result); err != nil {
		t.Fatal(err)
	}
	if err = repo.Complete(ctx, v.ID, result); err != nil {
		t.Fatal("duplicate callback", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE ocr_extracted_subjects SET grade_value=19 WHERE ocr_analysis_id=$1::uuid`, v.ID); err == nil {
		t.Fatal("immutable extracted grade changed")
	}
	if _, err = pool.Exec(ctx, `UPDATE ocr_analyses SET expected_subjects='[]'::jsonb WHERE id=$1::uuid`, v.ID); err == nil {
		t.Fatal("snapshot changed")
	}
	confirmed, err := repo.Decide(ctx, hash, v.ID, d.Version, "CONFIRMED")
	if err != nil || !confirmed.Ready {
		t.Fatal("confirmation", err)
	}
	d, err = draftRepo.Update(ctx, hash, "program", drafts.Change{Version: d.Version, Program: "IM"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.Current(ctx, hash)
	if err != nil || current.ID == v.ID || current.Status != "QUEUED" {
		t.Fatal("stale context reused", err)
	}
	if _, err = repo.Decide(ctx, hash, v.ID, d.Version, "CONFIRMED"); err == nil {
		t.Fatal("stale confirmation")
	}
	var status string
	pool.QueryRow(ctx, `SELECT status FROM ocr_analyses WHERE id=$1::uuid`, v.ID).Scan(&status)
	if status != "OBSOLETE" {
		t.Fatal("old analysis not obsolete")
	}
	// A rejection cannot be bypassed by switching away and back to a context.
	claim = httptest.NewRequest("POST", "/internal/ocr/"+current.ID+"/claim", strings.NewReader(`{}`))
	claim.Header.Set("Authorization", "Bearer "+secret)
	response = httptest.NewRecorder()
	internal.ServeHTTP(response, claim)
	if response.Code != 200 {
		t.Fatal("claim second context")
	}
	if err = repo.Complete(ctx, current.ID, result); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Decide(ctx, hash, current.ID, d.Version, "REJECTED"); err != nil {
		t.Fatal(err)
	}
	d, err = draftRepo.Update(ctx, hash, "program", drafts.Change{Version: d.Version, Program: "GL"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Current(ctx, hash); err == nil || !strings.Contains(err.Error(), "TRANSCRIPT_REPLACEMENT_REQUIRED") {
		t.Fatal("rejected version reactivated", err)
	}
	var bridge string
	if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&bridge); err != nil {
		t.Fatal(err)
	}
	if err = docs.Save(ctx, hash, d.Version, "TRANSCRIPT", documents.Version{ID: bridge, Key: "applications/" + d.ID + "/documents/TRANSCRIPT/" + bridge + ".pdf", Filename: "new-version.pdf", SHA256: strings.Repeat("2", 64), Size: 100}); err != nil {
		t.Fatal(err)
	}
	d, err = draftRepo.Current(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	current, err = repo.Current(ctx, hash)
	if err != nil || current.Status != "QUEUED" {
		t.Fatal("new version did not unlock analysis", err)
	}
	// A worker result refused by Go is terminal at once: FAILED/RESULT_REJECTED, no replay.
	claim = httptest.NewRequest("POST", "/internal/ocr/"+current.ID+"/claim", strings.NewReader(`{}`))
	claim.Header.Set("Authorization", "Bearer "+secret)
	response = httptest.NewRecorder()
	internal.ServeHTTP(response, claim)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "PROCESSING") {
		t.Fatal("claim rejected scenario", response.Body.String())
	}
	invalid := result
	invalid.Subjects = append([]Extraction{}, result.Subjects...)
	invalid.Subjects[0].Grade, invalid.Subjects[0].GradeRaw = "21", "21"
	post := func(body string) int {
		request := httptest.NewRequest("POST", "/internal/ocr/"+current.ID+"/result", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+secret)
		recorder := httptest.NewRecorder()
		internal.ServeHTTP(recorder, request)
		return recorder.Code
	}
	stale := invalid
	stale.Attempt = 9
	if code := post(string(jsonBytes(stale))); code != 422 {
		t.Fatal("stale invalid result", code)
	}
	var failure string
	pool.QueryRow(ctx, `SELECT status,coalesce(failure_code,'') FROM ocr_analyses WHERE id=$1::uuid`, current.ID).Scan(&status, &failure)
	if status != "PROCESSING" {
		t.Fatal("stale invalid attempt changed the analysis", status)
	}
	if code := post(string(jsonBytes(invalid))); code != 422 {
		t.Fatal("invalid result", code)
	}
	pool.QueryRow(ctx, `SELECT status,coalesce(failure_code,'') FROM ocr_analyses WHERE id=$1::uuid`, current.ID).Scan(&status, &failure)
	if status != "FAILED" || failure != "RESULT_REJECTED" {
		t.Fatal("rejected result not terminal", status, failure)
	}
	if code := post(string(jsonBytes(invalid))); code != 422 {
		t.Fatal("replayed invalid result", code)
	}
	if code := post(`{"contractVersion":`); code != 400 {
		t.Fatal("malformed callback", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE ocr_analyses SET status='PROCESSING' WHERE id=$1::uuid`, current.ID); err == nil {
		t.Fatal("rejected analysis reopened")
	}
	view, err := repo.Current(ctx, hash)
	if err != nil || view.Status != "FAILED" || view.Failure != "RESULT_REJECTED" || len(view.Subjects) != 0 {
		t.Fatal("candidate view of rejected result", err, view.Status, view.Failure)
	}
	if _, err = repo.Decide(ctx, hash, current.ID, d.Version, "CONFIRMED"); err == nil {
		t.Fatal("FAILED confirmation")
	}
	var replacement string
	if err = pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&replacement); err != nil {
		t.Fatal(err)
	}
	if err = docs.Save(ctx, hash, d.Version, "TRANSCRIPT", documents.Version{ID: replacement, Key: "applications/" + d.ID + "/documents/TRANSCRIPT/" + replacement + ".pdf", Filename: "replacement.pdf", SHA256: strings.Repeat("1", 64), Size: 100}); err != nil {
		t.Fatal(err)
	}
	d, err = draftRepo.Current(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := repo.Current(ctx, hash)
	if err != nil || replaced.ID == current.ID || replaced.DocumentVersion != replacement {
		t.Fatal("replacement reuses old analysis", err)
	}
	if err = pool.QueryRow(ctx, `SELECT expected_subjects FROM ocr_analyses WHERE id=$1::uuid`, replaced.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	late := Result{ContractVersion: 1, Attempt: 1, Status: "SUCCEEDED", Engine: "synthetic-contract", EngineVersion: "1"}
	for i, e := range expected {
		late.Subjects = append(late.Subjects, subjectOnRow(e, i))
	}
	if _, err = pool.Exec(ctx, `UPDATE ocr_analyses SET status='PROCESSING',attempt_number=1,started_at=now() WHERE id=$1::uuid`, replaced.ID); err != nil {
		t.Fatal(err)
	}
	d, err = draftRepo.Update(ctx, hash, "academic-background", drafts.Change{Version: d.Version, Qualification: "DT_EAP", GraduationYear: &year})
	if err != nil {
		t.Fatal(err)
	}
	if d.Program != nil {
		t.Fatal("invalidated program retained")
	}
	if err = repo.Complete(ctx, replaced.ID, late); err != nil {
		t.Fatal("late audit callback", err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM ocr_analyses WHERE id=$1::uuid`, replaced.ID).Scan(&status); err != nil || status != "OBSOLETE" {
		t.Fatal("late result activated", err)
	}
	if _, err = repo.Current(ctx, hash); err == nil {
		t.Fatal("incompatible context has current analysis")
	}
	if _, err = provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}

// subjectOnRow places each expected subject on its own table row (distinct cells).
func subjectOnRow(e Expected, row int) Extraction {
	valid, _ := validResult()
	s := valid.Subjects[0]
	s.SubjectID, s.Position, s.Detected = e.ID, e.Position, e.Name
	y := 0.1 + float64(row)*0.05
	s.Evidence.Subject = BBox{0.1, y, 0.5, y + 0.04}
	s.Evidence.Grade = BBox{0.55, y, 0.65, y + 0.04}
	s.Evidence.Coefficient = BBox{0.66, y, 0.75, y + 0.04}
	return s
}
