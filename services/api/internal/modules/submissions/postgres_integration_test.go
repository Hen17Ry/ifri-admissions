//go:build integration

package submissions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/analysis"
	"ifri-admissions/services/api/internal/modules/documents"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

const testOrigin = "http://localhost:5173"

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type harness struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	provider *goose.Provider
	drafts   *drafts.PostgresRepository
	docs     *documents.PostgresRepository
	analyses analysis.Repository
	subs     *Repository
	seq      int
}

// isolated creates a private schema, migrates it up to `before`, runs `between`, then migrates
// to the latest version and seeds it.
func isolated(t *testing.T, ctx context.Context, before int64, between string) *harness {
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
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	schema := "ifri_submission_test_" + hex.EncodeToString(token)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("cleanup failed")
		}
	})
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
	if before > 0 {
		if _, err = provider.UpTo(ctx, before); err != nil {
			t.Fatal(err)
		}
	}
	if between != "" {
		if _, err = pool.Exec(ctx, between); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err = seeds.Development(ctx, pool, "test"); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, ctx: ctx, pool: pool, provider: provider, drafts: drafts.NewRepository(pool), docs: documents.NewRepository(pool),
		analyses: analysis.Repository{Pool: pool, Threshold: 0.85}, subs: &Repository{Pool: pool, Logger: quiet}}
}

func (h *harness) uuid() string {
	var id string
	if err := h.pool.QueryRow(h.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

// applicant describes one synthetic candidate; grades are (note, coefficient) per expected subject.
type applicant struct {
	email, phone, qualification, program, transcriptSHA string
	grades                                              [][2]string
	evidence                                            func(row int) map[string]any // nil: generic path through analysis.Complete
	skipDocument                                        string
	stopBefore                                          string // "analysis" (left QUEUED) or "confirmation" (SUCCEEDED, PENDING)
}

func (h *harness) person(email string) applicant {
	h.seq++
	return applicant{email: email, phone: fmt.Sprintf("+22901%08d", h.seq), qualification: "DT_DWM", program: "GL",
		transcriptSHA: fmt.Sprintf("%064x", h.seq), grades: [][2]string{{"12.50", "2"}, {"14", "3"}, {"9.75", "1"}}}
}

func (h *harness) mustDraft(hash []byte) drafts.Draft {
	d, err := h.drafts.Current(h.ctx, hash)
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

// prepare builds a complete draft with a confirmed analysis unless the applicant stops earlier.
func (h *harness) prepare(a applicant) ([]byte, drafts.Draft) {
	t := h.t
	t.Helper()
	_, hash, _ := drafts.NewSecret()
	d, err := h.drafts.Create(h.ctx, "DEV_LOCAL", hash, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = h.drafts.Update(h.ctx, hash, "profile", drafts.Change{Version: d.Version, Profile: &drafts.Profile{LastName: "Synthétique", FirstNames: "Candidat", Email: a.email, Phone: a.phone}}); err != nil {
		t.Fatal(err)
	}
	year := 2025
	if d, err = h.drafts.Update(h.ctx, hash, "academic-background", drafts.Change{Version: d.Version, Qualification: a.qualification, GraduationYear: &year}); err != nil {
		t.Fatal(err)
	}
	if d, err = h.drafts.Update(h.ctx, hash, "program", drafts.Change{Version: d.Version, Program: a.program}); err != nil {
		t.Fatal(err)
	}
	for _, code := range documentTypes {
		if code == a.skipDocument {
			continue
		}
		sha := fmt.Sprintf("%064x", 1_000_000+h.seq*10+len(code))
		if code == "TRANSCRIPT" {
			sha = a.transcriptSHA
		}
		id := h.uuid()
		if err = h.docs.Save(h.ctx, hash, d.Version, code, documents.Version{ID: id, Key: "applications/" + d.ID + "/documents/" + code + "/" + id + ".pdf", Filename: "synthetic.pdf", SHA256: sha, Size: 100}); err != nil {
			t.Fatal(err)
		}
		d = h.mustDraft(hash)
	}
	if a.skipDocument == "TRANSCRIPT" || a.stopBefore == "analysis" {
		return hash, d
	}
	h.complete(hash, a)
	if a.stopBefore == "confirmation" {
		return hash, d
	}
	h.confirm(hash)
	return hash, h.mustDraft(hash)
}

// complete answers the queued analysis of the current transcript like the worker would.
func (h *harness) complete(hash []byte, a applicant) string {
	t := h.t
	t.Helper()
	v, err := h.analyses.Current(h.ctx, hash)
	if err != nil || v.Status != "QUEUED" {
		t.Fatalf("analysis not queued: %v %v", v.Status, err)
	}
	if _, err = h.pool.Exec(h.ctx, `UPDATE ocr_analyses SET status='PROCESSING',attempt_number=1,started_at=now() WHERE id=$1::uuid`, v.ID); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = h.pool.QueryRow(h.ctx, `SELECT expected_subjects FROM ocr_analyses WHERE id=$1::uuid`, v.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var expected []analysis.Expected
	if err = json.Unmarshal(raw, &expected); err != nil || len(expected) != len(a.grades) {
		t.Fatalf("expected subjects: %d for %d grades", len(expected), len(a.grades))
	}
	if a.evidence != nil {
		// Cell evidence shapes are produced by the worker; the snapshot only maps them.
		for i, e := range expected {
			evidence, _ := json.Marshal(a.evidence(i))
			if _, err = h.pool.Exec(h.ctx, `INSERT INTO ocr_extracted_subjects(ocr_analysis_id,subject_id,position,detected_subject_text,grade_raw,grade_value,coefficient_raw,coefficient_value,page_number,subject_confidence,grade_confidence,coefficient_confidence,overall_confidence,evidence) VALUES($1::uuid,$2::uuid,$3,'Matière',$4,$5::numeric,$6,$7::numeric,1,1,1,1,1,$8::jsonb)`, v.ID, e.ID, e.Position, a.grades[i][0], a.grades[i][0], a.grades[i][1], a.grades[i][1], evidence); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = h.pool.Exec(h.ctx, `UPDATE ocr_analyses SET status='SUCCEEDED',engine='synthetic',engine_version='1',completed_at=now(),result_digest=repeat('a',64) WHERE id=$1::uuid`, v.ID); err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	result := analysis.Result{ContractVersion: 1, Attempt: 1, Status: "SUCCEEDED", Engine: "synthetic-contract", EngineVersion: "1"}
	for i, e := range expected {
		y := 0.1 + float64(i)*0.05
		result.Subjects = append(result.Subjects, analysis.Extraction{SubjectID: e.ID, Position: e.Position, Detected: e.Name,
			GradeRaw: a.grades[i][0], Grade: a.grades[i][0], CoefficientRaw: a.grades[i][1], Coefficient: a.grades[i][1], Page: 1,
			SubjectConfidence: 1, GradeConfidence: 1, CoefficientConfidence: 1, Confidence: 1,
			Evidence: analysis.Evidence{Subject: analysis.BBox{X0: 0.1, Y0: y, X1: 0.5, Y1: y + 0.04}, Grade: analysis.BBox{X0: 0.55, Y0: y, X1: 0.65, Y1: y + 0.04}, Coefficient: analysis.BBox{X0: 0.66, Y0: y, X1: 0.75, Y1: y + 0.04}, System: "NORMALIZED_TOP_LEFT"}})
	}
	if err = h.analyses.Complete(h.ctx, v.ID, result); err != nil {
		t.Fatal(err)
	}
	return v.ID
}

func (h *harness) confirm(hash []byte) {
	h.t.Helper()
	d := h.mustDraft(hash)
	v, err := h.analyses.Current(h.ctx, hash)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err = h.analyses.Decide(h.ctx, hash, v.ID, d.Version, "CONFIRMED"); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx, query, args...).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func problemCode(err error) string {
	var p *drafts.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	if err == nil {
		return "nil"
	}
	return "unexpected: " + err.Error()
}

func TestPostgresSubmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	// A campaign opening at 00:30 on 1 January in Porto-Novo (UTC+1) still starts the previous
	// day in UTC: the backfill must use the local opening year.
	h := isolated(t, ctx, 5, `INSERT INTO admission_campaigns(code,name,academic_year,starts_at,ends_at,status) VALUES ('BACKFILL_CHECK','Backfill','2026-2027','2026-12-31T23:30:00Z','2027-06-01T00:00:00Z','CLOSED')`)
	t.Run("numbering year backfilled from the opening date", func(t *testing.T) {
		if n := h.count(`SELECT numbering_year FROM admission_campaigns WHERE code='BACKFILL_CHECK'`); n != 2027 {
			t.Fatal("numbering year", n)
		}
		if _, err := h.pool.Exec(ctx, `UPDATE admission_campaigns SET numbering_year=1999 WHERE code='BACKFILL_CHECK'`); err == nil {
			t.Fatal("numbering year outside 2000-9999 accepted")
		}
	})

	t.Run("each precondition has its own code", func(t *testing.T) {
		if _, _, err := h.subs.Submit(ctx, drafts.HashSecret("unknown-session"), 1); problemCode(err) != "DRAFT_SESSION_REQUIRED" {
			t.Fatal(problemCode(err))
		}
		// Profile and documents only.
		_, hash, _ := drafts.NewSecret()
		d, _ := h.drafts.Create(ctx, "DEV_LOCAL", hash, time.Hour)
		_, _, err := h.subs.Submit(ctx, hash, d.Version)
		var p *drafts.Problem
		if !errors.As(err, &p) || p.Code != "APPLICATION_INCOMPLETE" || p.Status != 422 {
			t.Fatal(problemCode(err))
		}
		for _, field := range []string{"lastName", "email", "phone", "qualification", "graduationYear", "program", "document.TRANSCRIPT", "document.PARENTAL_COMMITMENT"} {
			if p.Fields[field] == "" {
				t.Fatal("missing field", field, p.Fields)
			}
		}
		missing := h.person("missing-document@example.invalid")
		missing.skipDocument = "IDENTITY_DOCUMENT"
		hash, d = h.prepare(missing)
		if _, _, err = h.subs.Submit(ctx, hash, d.Version); !errors.As(err, &p) || p.Code != "APPLICATION_INCOMPLETE" || len(p.Fields) != 1 || p.Fields["document.IDENTITY_DOCUMENT"] == "" {
			t.Fatal(problemCode(err), p.Fields)
		}

		queued := h.person("queued@example.invalid")
		queued.stopBefore = "analysis"
		hash, d = h.prepare(queued)
		if _, _, err = h.subs.Submit(ctx, hash, d.Version); problemCode(err) != "TRANSCRIPT_ANALYSIS_REQUIRED" {
			t.Fatal("queued analysis:", problemCode(err))
		}
		pending := h.person("pending@example.invalid")
		pending.stopBefore = "confirmation"
		hash, d = h.prepare(pending)
		if _, _, err = h.subs.Submit(ctx, hash, d.Version); problemCode(err) != "TRANSCRIPT_ANALYSIS_REQUIRED" {
			t.Fatal("unconfirmed analysis:", problemCode(err))
		}

		ready := h.person("ready@example.invalid")
		hash, d = h.prepare(ready)
		if _, _, err = h.subs.Submit(ctx, hash, d.Version-1); problemCode(err) != "VERSION_CONFLICT" {
			t.Fatal(problemCode(err))
		}
		for _, closing := range []string{`UPDATE admission_campaigns SET status='CLOSED' WHERE code='DEV_LOCAL'`, `UPDATE admission_campaigns SET ends_at=now()-interval '1 minute' WHERE code='DEV_LOCAL'`} {
			if _, err = h.pool.Exec(ctx, closing); err != nil {
				t.Fatal(err)
			}
			_, _, err = h.subs.Submit(ctx, hash, d.Version)
			if _, restore := h.pool.Exec(ctx, `UPDATE admission_campaigns SET status='OPEN',ends_at='2100-01-01T00:00:00Z' WHERE code='DEV_LOCAL'`); restore != nil {
				t.Fatal(restore)
			}
			if problemCode(err) != "CAMPAIGN_CLOSED" {
				t.Fatal(closing, problemCode(err))
			}
		}
		// The couple stops being offered (offer disabled): checked again inside the transaction.
		if _, err = h.pool.Exec(ctx, `UPDATE campaign_programs SET enabled=false WHERE program_id=(SELECT id FROM programs WHERE code='GL')`); err != nil {
			t.Fatal(err)
		}
		_, _, err = h.subs.Submit(ctx, hash, d.Version)
		if _, restore := h.pool.Exec(ctx, `UPDATE campaign_programs SET enabled=true`); restore != nil {
			t.Fatal(restore)
		}
		if problemCode(err) != "APPLICATION_NOT_ELIGIBLE" {
			t.Fatal(problemCode(err))
		}
		// A confirmed analysis followed by a new transcript version is outdated, not missing.
		id := h.uuid()
		if err = h.docs.Save(ctx, hash, d.Version, "TRANSCRIPT", documents.Version{ID: id, Key: "applications/" + d.ID + "/documents/TRANSCRIPT/" + id + ".pdf", Filename: "v2.pdf", SHA256: strings.Repeat("e", 64), Size: 100}); err != nil {
			t.Fatal(err)
		}
		d = h.mustDraft(hash)
		if _, _, err = h.subs.Submit(ctx, hash, d.Version); problemCode(err) != "TRANSCRIPT_ANALYSIS_OUTDATED" {
			t.Fatal(problemCode(err))
		}
		if n := h.count(`SELECT count(*) FROM application_submissions`); n != 0 {
			t.Fatal("refused submissions persisted", n)
		}
	})

	t.Run("admission rules changed: analysis invalidated and queued again, submission refused", func(t *testing.T) {
		hash, d := h.prepare(h.person("rules@example.invalid"))
		before, _ := h.analyses.Current(ctx, hash)
		// Publish a second grid for the GL offer; triggers protect published grids in production.
		if _, err := h.pool.Exec(ctx, `ALTER TABLE rule_set_versions DISABLE TRIGGER rule_set_guard;
		 INSERT INTO rule_set_versions(campaign_program_id,version,source,status)
		 SELECT cp.id,2,'Simulated grid change','DRAFT' FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id WHERE p.code='GL';
		 INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible)
		 SELECT n.id,e.origin_qualification_id,e.eligible FROM rule_set_versions n JOIN rule_set_versions o ON o.campaign_program_id=n.campaign_program_id AND o.version=1
		 JOIN program_qualification_eligibilities e ON e.rule_set_version_id=o.id JOIN campaign_programs cp ON cp.id=n.campaign_program_id JOIN programs p ON p.id=cp.program_id WHERE p.code='GL' AND n.version=2;
		 INSERT INTO expected_subject_rules(eligibility_id,subject_id,position)
		 SELECT ne.id,r.subject_id,r.position FROM expected_subject_rules r JOIN program_qualification_eligibilities oe ON oe.id=r.eligibility_id
		 JOIN rule_set_versions o ON o.id=oe.rule_set_version_id AND o.version=1 JOIN rule_set_versions n ON n.campaign_program_id=o.campaign_program_id AND n.version=2
		 JOIN program_qualification_eligibilities ne ON ne.rule_set_version_id=n.id AND ne.origin_qualification_id=oe.origin_qualification_id
		 JOIN campaign_programs cp ON cp.id=n.campaign_program_id JOIN programs p ON p.id=cp.program_id WHERE p.code='GL';
		 UPDATE rule_set_versions v SET status='DRAFT' FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id WHERE v.campaign_program_id=cp.id AND p.code='GL' AND v.version=1;
		 UPDATE rule_set_versions v SET status='PUBLISHED' FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id WHERE v.campaign_program_id=cp.id AND p.code='GL' AND v.version=2;
		 ALTER TABLE rule_set_versions ENABLE TRIGGER rule_set_guard;`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := h.subs.Submit(ctx, hash, d.Version); problemCode(err) != "ADMISSION_RULES_CHANGED" {
			t.Fatal(problemCode(err))
		}
		var status string
		if err := h.pool.QueryRow(ctx, `SELECT status FROM ocr_analyses WHERE id=$1::uuid`, before.ID).Scan(&status); err != nil || status != "OBSOLETE" {
			t.Fatal("old analysis kept", status, err)
		}
		after, err := h.analyses.Current(ctx, hash)
		if err != nil || after.ID == before.ID || after.Status != "QUEUED" || after.DocumentVersion != before.DocumentVersion {
			t.Fatal("reanalysis not queued", after.Status, err)
		}
		if h.count(`SELECT count(*) FROM ocr_analyses o JOIN rule_set_versions v ON v.id=o.rule_grid_version_id WHERE o.id=$1::uuid AND v.version=2`, after.ID) != 1 {
			t.Fatal("reanalysis not bound to the new grid")
		}
		if d2 := h.mustDraft(hash); d2.State != "DRAFT" || d2.Version != d.Version {
			t.Fatal("application changed by a refused submission")
		}
		h.complete(hash, h.person("unused"))
		h.confirm(hash)
		receipt, _, err := h.subs.Submit(ctx, hash, h.mustDraft(hash).Version)
		if err != nil {
			t.Fatal(err)
		}
		if h.count(`SELECT count(*) FROM application_submissions s JOIN rule_set_versions v ON v.id=s.rule_set_version_id JOIN applications a ON a.id=s.application_id WHERE v.version=2 AND a.application_number=$1`, receipt.ApplicationNumber) != 1 {
			t.Fatal("snapshot not bound to the new grid")
		}
	})

	var submittedHash []byte
	var submitted drafts.Draft
	var number string
	t.Run("successful submission writes number, exact average, snapshot and audit", func(t *testing.T) {
		submittedHash, submitted = h.prepare(h.person("success@example.invalid"))
		counter := h.count(`SELECT coalesce(max(last_serial),0) FROM application_number_counters c JOIN programs p ON p.id=c.program_id WHERE p.code='GL' AND c.numbering_year=2000`)
		receipt, out, err := h.subs.Submit(ctx, submittedHash, submitted.Version)
		if err != nil || out.Replay {
			t.Fatal(err)
		}
		number = receipt.ApplicationNumber
		if want := fmt.Sprintf("IFRI-2000-GL-%06d", counter+1); number != want {
			t.Fatal(number, want)
		}
		if receipt.Program.Code != "GL" || receipt.Candidate.LastName != "Synthétique" || receipt.SubmittedAt.IsZero() || receipt.GraduationYear != 2025 {
			t.Fatalf("receipt %+v", receipt)
		}
		var state, weighted, coefficients, average string
		var exact bool
		var version int64
		if err = h.pool.QueryRow(ctx, `SELECT a.state,a.version,s.weighted_sum::text,s.coefficient_sum::text,s.average::text,s.average_is_exact
		 FROM applications a JOIN application_submissions s ON s.application_id=a.id WHERE a.id=$1::uuid`, submitted.ID).Scan(&state, &version, &weighted, &coefficients, &average, &exact); err != nil {
			t.Fatal(err)
		}
		// 12.50 x 2 + 14 x 3 + 9.75 x 1 = 76.75 ; 76.75 / 6 = 12.791666...
		if state != "SUBMITTED" || version != submitted.Version+1 || weighted != "76.75" || coefficients != "6" || average != "12.791666666666666666666666666666" || exact {
			t.Fatal(state, version, weighted, coefficients, average, exact)
		}
		if h.count(`SELECT count(*) FROM application_submission_subjects s JOIN application_submissions x ON x.id=s.submission_id WHERE x.application_id=$1::uuid AND s.validation_method='DIRECT_READ' AND s.reconstructed_cells='{}'`, submitted.ID) != 3 {
			t.Fatal("subjects not copied with their method")
		}
		if h.count(`SELECT count(*) FROM application_submission_documents s JOIN application_submissions x ON x.id=s.submission_id WHERE x.application_id=$1::uuid`, submitted.ID) != 4 {
			t.Fatal("documents not referenced")
		}
		var status, confirmation string
		if err = h.pool.QueryRow(ctx, `SELECT o.status,o.candidate_confirmation FROM ocr_analyses o JOIN application_submissions s ON s.ocr_analysis_id=o.id WHERE s.application_id=$1::uuid`, submitted.ID).Scan(&status, &confirmation); err != nil || status != "SUCCEEDED" || confirmation != "CONFIRMED" {
			t.Fatal("submission made its analysis obsolete", status, err)
		}
		var metadata string
		if err = h.pool.QueryRow(ctx, `SELECT metadata::text FROM audit_events WHERE event_type='APPLICATION_SUBMITTED' AND actor_type='CANDIDATE' AND actor_id IS NOT NULL AND target_type='APPLICATION' AND target_id=$1::uuid`, submitted.ID).Scan(&metadata); err != nil {
			t.Fatal("audit event", err)
		}
		for _, pii := range []string{"success@example.invalid", "Synthétique", "Candidat", "+229", number, "76.75", "12.79"} {
			if strings.Contains(metadata, pii) {
				t.Fatal("audit metadata leaks", pii)
			}
		}
		var expiresAt, endsAt time.Time
		if err = h.pool.QueryRow(ctx, `SELECT s.expires_at,k.ends_at FROM application_draft_sessions s JOIN applications a ON a.id=s.application_id JOIN admission_campaigns k ON k.id=a.campaign_id WHERE s.secret_hash=$1`, submittedHash).Scan(&expiresAt, &endsAt); err != nil || !expiresAt.Equal(endsAt) || !receipt.SessionExpiresAt.Equal(endsAt) {
			t.Fatal("read-only session not extended to the campaign end", expiresAt, err)
		}
		replay, out, err := h.subs.Submit(ctx, submittedHash, 1)
		if err != nil || !out.Replay || !same(replay, receipt) {
			t.Fatal("replay differs", err)
		}
		if again, err := h.subs.Receipt(ctx, submittedHash); err != nil || !same(again, receipt) {
			t.Fatal("receipt differs", err)
		}
	})

	t.Run("short campaign: read-only session lasts at least seven days", func(t *testing.T) {
		hash, d := h.prepare(h.person("short@example.invalid"))
		if _, err := h.pool.Exec(ctx, `UPDATE admission_campaigns SET ends_at=now()+interval '1 day' WHERE code='DEV_LOCAL'`); err != nil {
			t.Fatal(err)
		}
		receipt, _, err := h.subs.Submit(ctx, hash, d.Version)
		if _, restore := h.pool.Exec(ctx, `UPDATE admission_campaigns SET ends_at='2100-01-01T00:00:00Z' WHERE code='DEV_LOCAL'`); restore != nil {
			t.Fatal(restore)
		}
		if err != nil || receipt.SessionExpiresAt.Before(time.Now().Add(7*24*time.Hour-time.Minute)) || receipt.SessionExpiresAt.After(time.Now().Add(7*24*time.Hour+time.Minute)) {
			t.Fatal("minimum read-only duration", receipt.SessionExpiresAt, err)
		}
	})

	t.Run("submitted application is locked through every candidate route", func(t *testing.T) {
		code := func(err error) string { return problemCode(err) }
		if _, err := h.drafts.Update(ctx, submittedHash, "profile", drafts.Change{Version: 1, Profile: &submitted.Profile}); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("draft update", code(err))
		}
		id := h.uuid()
		if err := h.docs.Save(ctx, submittedHash, 1, "TRANSCRIPT", documents.Version{ID: id, Key: "applications/" + submitted.ID + "/documents/TRANSCRIPT/" + id + ".pdf", Filename: "late.pdf", SHA256: strings.Repeat("f", 64), Size: 100}); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("document save", code(err))
		}
		if _, err := h.analyses.Current(ctx, submittedHash); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("analysis read", code(err))
		}
		if _, err := h.analyses.Decide(ctx, submittedHash, h.uuid(), 1, "REJECTED"); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("analysis decision", code(err))
		}
		service := documents.NewService(h.docs, nil, nil, 1<<20, quiet)
		if _, err := service.List(ctx, submittedHash); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("document list", code(err))
		}
		if _, _, err := service.Content(ctx, submittedHash, "TRANSCRIPT"); code(err) != "APPLICATION_LOCKED" {
			t.Fatal("document content", code(err))
		}

		// Same through HTTP: only GET /current and GET /current/submission still answer 200.
		mux := http.NewServeMux()
		drafts.Register(mux, h.drafts, time.Hour, testOrigin, false, quiet)
		documents.Register(mux, service, testOrigin, false, quiet)
		analysis.Handler{Repo: h.analyses, Origin: testOrigin}.RegisterPublic(mux)
		Register(mux, h.subs, testOrigin, false, quiet)
		token, secret, _ := drafts.NewSecret()
		// Swap the stored hash for a token we know, keeping the same session row.
		if _, err := h.pool.Exec(ctx, `UPDATE application_draft_sessions SET secret_hash=$2 WHERE secret_hash=$1`, submittedHash, secret); err != nil {
			t.Fatal(err)
		}
		submittedHash = secret
		apps := h.count(`SELECT count(*) FROM applications`)
		base := drafts.CookiePath
		for _, tc := range []struct {
			method, path, body, contentType string
			status                          int
			code                            string
		}{
			{"GET", base + "/current", "", "", 200, ""},
			{"GET", base + "/current/submission", "", "", 200, ""},
			{"POST", base, `{"campaign":"DEV_LOCAL"}`, "application/json", 200, ""},
			{"PATCH", base + "/current/profile", `{"version":99,"profile":{"lastName":"X","firstNames":"Y","email":"x@example.invalid","phone":"+22901000000"}}`, "application/json", 409, "APPLICATION_LOCKED"},
			{"PATCH", base + "/current/academic-background", `{"version":99,"qualification":"BAC_C","graduationYear":2024}`, "application/json", 409, "APPLICATION_LOCKED"},
			{"PATCH", base + "/current/program", `{"version":99,"program":"IA"}`, "application/json", 409, "APPLICATION_LOCKED"},
			{"POST", base + "/current/documents/TRANSCRIPT", "--x--\r\n", "multipart/form-data; boundary=x", 409, "APPLICATION_LOCKED"},
			{"GET", base + "/current/documents", "", "", 409, "APPLICATION_LOCKED"},
			{"GET", base + "/current/documents/TRANSCRIPT/content", "", "", 409, "APPLICATION_LOCKED"},
			{"GET", base + "/current/transcript-analysis", "", "", 409, "APPLICATION_LOCKED"},
			{"POST", base + "/current/transcript-analysis/confirm", `{"analysisId":"00000000-0000-0000-0000-000000000000","version":99}`, "application/json", 409, "APPLICATION_LOCKED"},
			{"POST", base + "/current/submission", `{"version":99}`, "application/json", 200, ""},
		} {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Origin", testOrigin)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			if tc.path == base+"/current/documents/TRANSCRIPT" {
				r.Header.Set("X-Application-Version", "99")
			}
			r.AddCookie(&http.Cookie{Name: drafts.CookieName, Value: token})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status || (tc.code != "" && !strings.Contains(w.Body.String(), `"`+tc.code+`"`)) {
				t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
			}
			if tc.status == 200 && !strings.Contains(w.Body.String(), number) {
				t.Fatalf("%s %s: number missing", tc.method, tc.path)
			}
		}
		if h.count(`SELECT count(*) FROM applications`) != apps {
			t.Fatal("a new draft was created from a submitted session")
		}
	})

	t.Run("database refuses any change to a submission", func(t *testing.T) {
		for _, tc := range []struct{ statement, message string }{
			{`UPDATE applications SET graduation_year=2020 WHERE id=$1::uuid`, "Submitted application is immutable"},
			{`UPDATE applications SET state='DRAFT',application_number=NULL,submitted_at=NULL WHERE id=$1::uuid`, "Submitted application is immutable"},
			{`DELETE FROM applications WHERE id=$1::uuid`, "Submitted application is immutable"},
			{`UPDATE candidates SET last_name='Changed' WHERE id=(SELECT candidate_id FROM applications WHERE id=$1::uuid)`, "Candidate of a submitted application is immutable"},
			{`UPDATE application_submissions SET average=0 WHERE application_id=$1::uuid`, "append-only"},
			{`DELETE FROM application_submissions WHERE application_id=$1::uuid`, "append-only"},
			{`UPDATE application_submission_subjects SET grade_value=20 WHERE submission_id=(SELECT id FROM application_submissions WHERE application_id=$1::uuid)`, "append-only"},
			{`DELETE FROM application_submission_documents WHERE submission_id=(SELECT id FROM application_submissions WHERE application_id=$1::uuid)`, "append-only"},
			{`UPDATE audit_events SET metadata='{}' WHERE target_id=$1::uuid`, "append-only"},
			{`DELETE FROM audit_events WHERE target_id=$1::uuid`, "append-only"},
			{`UPDATE ocr_analyses SET status='OBSOLETE' WHERE id=(SELECT ocr_analysis_id FROM application_submissions WHERE application_id=$1::uuid)`, "Submitted analysis is immutable"},
			{`INSERT INTO ocr_extracted_subjects(ocr_analysis_id,subject_id,position,detected_subject_text,grade_raw,grade_value,coefficient_raw,coefficient_value,page_number,subject_confidence,grade_confidence,coefficient_confidence,overall_confidence,evidence)
			 SELECT s.ocr_analysis_id,sub.id,99,'x','1',1,'1',1,1,1,1,1,1,'{}' FROM application_submissions s CROSS JOIN LATERAL (SELECT id FROM subjects WHERE id NOT IN (SELECT subject_id FROM ocr_extracted_subjects WHERE ocr_analysis_id=s.ocr_analysis_id) LIMIT 1) sub WHERE s.application_id=$1::uuid`, "OCR evidence is immutable"},
			{`UPDATE document_versions SET replaced_at=now() WHERE application_document_id IN (SELECT id FROM application_documents WHERE application_id=$1::uuid)`, "Documents of a submitted application are immutable"},
			{`UPDATE application_number_counters SET last_serial=last_serial-1 WHERE $1::uuid IS NOT NULL`, "only increase"},
			{`DELETE FROM application_number_counters WHERE $1::uuid IS NOT NULL`, "only increase"},
		} {
			tx, err := h.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, tc.statement, submitted.ID)
			if err == nil {
				err = tx.Commit(ctx)
			}
			_ = tx.Rollback(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatal("accepted or wrong guard:", tc.statement, err)
			}
		}
		for _, table := range []string{"application_submission_subjects", "audit_events", "ocr_extracted_subjects", "application_submission_flags"} {
			if _, err := h.pool.Exec(ctx, "TRUNCATE "+table+" CASCADE"); err == nil {
				t.Fatal("truncate accepted:", table)
			}
		}
		// A draft confirmed analysis keeps its evidence closed as well.
		hash, _ := h.prepare(h.person("evidence@example.invalid"))
		v, _ := h.analyses.Current(ctx, hash)
		if _, err := h.pool.Exec(ctx, `INSERT INTO ocr_extracted_subjects(ocr_analysis_id,subject_id,position,detected_subject_text,grade_raw,grade_value,coefficient_raw,coefficient_value,page_number,subject_confidence,grade_confidence,coefficient_confidence,overall_confidence,evidence)
		 SELECT $1::uuid,id,99,'x','1',1,'1',1,1,1,1,1,1,'{}' FROM subjects WHERE id NOT IN (SELECT subject_id FROM ocr_extracted_subjects WHERE ocr_analysis_id=$1::uuid) LIMIT 1`, v.ID); err == nil || !strings.Contains(err.Error(), "OCR evidence is immutable") {
			t.Fatal("subject added to a confirmed analysis")
		}
		// SUBMITTED requires a number and a date at once, and a snapshot at commit.
		_, d := h.prepare(h.person("constraint@example.invalid"))
		if _, err := h.pool.Exec(ctx, `UPDATE applications SET state='SUBMITTED' WHERE id=$1::uuid`, d.ID); err == nil {
			t.Fatal("SUBMITTED without number")
		}
		if _, err := h.pool.Exec(ctx, `UPDATE applications SET state='SUBMITTED',application_number='IFRI-2000-GL-999999',submitted_at=now() WHERE id=$1::uuid`, d.ID); err == nil {
			t.Fatal("SUBMITTED without snapshot")
		}
		if _, err := h.pool.Exec(ctx, `UPDATE applications SET application_number='IFRI-2000-GL-999998' WHERE id=$1::uuid`, d.ID); err == nil {
			t.Fatal("draft numbered")
		}
	})

	t.Run("concurrent submissions of one application: one number, one snapshot", func(t *testing.T) {
		hash, d := h.prepare(h.person("double-click@example.invalid"))
		receipts, outcomes, errs := concurrently(8, func(int) (Receipt, Outcome, error) { return h.subs.Submit(ctx, hash, d.Version) })
		replays := 0
		for i := range receipts {
			if errs[i] != nil {
				t.Fatal(errs[i])
			}
			if !same(receipts[i], receipts[0]) {
				t.Fatal("different responses", receipts[i].ApplicationNumber, receipts[0].ApplicationNumber)
			}
			if outcomes[i].Replay {
				replays++
			}
		}
		if replays != 7 {
			t.Fatal("replays", replays)
		}
		if h.count(`SELECT count(*) FROM application_submissions WHERE application_id=$1::uuid`, d.ID) != 1 || h.count(`SELECT count(*) FROM audit_events WHERE target_id=$1::uuid`, d.ID) != 1 {
			t.Fatal("duplicate snapshot or audit")
		}
	})

	t.Run("parallel submissions of distinct applications: distinct, contiguous serials", func(t *testing.T) {
		type prepared struct {
			hash    []byte
			version int64
		}
		ready := []prepared{}
		for i := range 6 {
			hash, d := h.prepare(h.person(fmt.Sprintf("parallel-%d@example.invalid", i)))
			ready = append(ready, prepared{hash, d.Version})
		}
		receipts, _, errs := concurrently(len(ready), func(i int) (Receipt, Outcome, error) { return h.subs.Submit(ctx, ready[i].hash, ready[i].version) })
		seen := map[string]bool{}
		low, high := 1_000_000, 0
		for i, r := range receipts {
			if errs[i] != nil {
				t.Fatal(errs[i])
			}
			seen[r.ApplicationNumber] = true
			var serial int
			fmt.Sscanf(r.ApplicationNumber[len(r.ApplicationNumber)-6:], "%d", &serial)
			low, high = min(low, serial), max(high, serial)
		}
		if len(seen) != len(ready) || high-low+1 != len(ready) {
			t.Fatal("serials", seen)
		}
	})

	t.Run("rollback: an injected failure leaves no number, snapshot or audit", func(t *testing.T) {
		hash, d := h.prepare(h.person("rollback@example.invalid"))
		serial := h.count(`SELECT last_serial FROM application_number_counters c JOIN programs p ON p.id=c.program_id WHERE p.code='GL' AND numbering_year=2000`)
		var expires time.Time
		if err := h.pool.QueryRow(ctx, `SELECT expires_at FROM application_draft_sessions WHERE secret_hash=$1`, hash).Scan(&expires); err != nil {
			t.Fatal(err)
		}
		if _, err := h.pool.Exec(ctx, `CREATE FUNCTION injected_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END; $$;
		 CREATE TRIGGER injected_failure BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION injected_failure();`); err != nil {
			t.Fatal(err)
		}
		_, _, err := h.subs.Submit(ctx, hash, d.Version)
		if _, drop := h.pool.Exec(ctx, `DROP TRIGGER injected_failure ON audit_events; DROP FUNCTION injected_failure();`); drop != nil {
			t.Fatal(drop)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Message, "injected failure") {
			t.Fatal("failure not injected", err)
		}
		after := h.mustDraft(hash)
		var stillExpires time.Time
		if err = h.pool.QueryRow(ctx, `SELECT expires_at FROM application_draft_sessions WHERE secret_hash=$1`, hash).Scan(&stillExpires); err != nil {
			t.Fatal(err)
		}
		if after.State != "DRAFT" || after.ApplicationNumber != nil || after.Version != d.Version || !stillExpires.Equal(expires) ||
			h.count(`SELECT count(*) FROM application_submissions WHERE application_id=$1::uuid`, d.ID) != 0 ||
			h.count(`SELECT count(*) FROM application_submission_flags WHERE application_id=$1::uuid`, d.ID) != 0 ||
			h.count(`SELECT last_serial FROM application_number_counters c JOIN programs p ON p.id=c.program_id WHERE p.code='GL' AND numbering_year=2000`) != serial {
			t.Fatal("partial submission persisted")
		}
		receipt, _, err := h.subs.Submit(ctx, hash, d.Version)
		if err != nil || receipt.ApplicationNumber != fmt.Sprintf("IFRI-2000-GL-%06d", serial+1) {
			t.Fatal("retry after rollback", receipt.ApplicationNumber, err)
		}
	})

	t.Run("full precision: equal at two decimals, different internally", func(t *testing.T) {
		periodic := h.person("periodic@example.invalid")
		periodic.grades = [][2]string{{"10", "1"}, {"10", "1"}, {"11", "1"}}
		flat := h.person("flat@example.invalid")
		flat.grades = [][2]string{{"10.33", "1"}, {"10.33", "1"}, {"10.33", "1"}}
		ids := []string{}
		for _, a := range []applicant{periodic, flat} {
			hash, d := h.prepare(a)
			if _, _, err := h.subs.Submit(ctx, hash, d.Version); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, d.ID)
		}
		var first, second string
		var firstExact, secondExact, sameAtTwo, crossGreater bool
		if err := h.pool.QueryRow(ctx, `SELECT a.average::text,a.average_is_exact,b.average::text,b.average_is_exact,trunc(a.average,2)=trunc(b.average,2),a.weighted_sum*b.coefficient_sum>b.weighted_sum*a.coefficient_sum
		 FROM application_submissions a,application_submissions b WHERE a.application_id=$1::uuid AND b.application_id=$2::uuid`, ids[0], ids[1]).Scan(&first, &firstExact, &second, &secondExact, &sameAtTwo, &crossGreater); err != nil {
			t.Fatal(err)
		}
		if first != "10.333333333333333333333333333333" || firstExact || second != "10.330000000000000000000000000000" || !secondExact || !sameAtTwo || !crossGreater {
			t.Fatal(first, firstExact, second, secondExact, sameAtTwo, crossGreater)
		}
	})

	t.Run("cell evidence keeps its validation method; unread series is flagged", func(t *testing.T) {
		bac := h.person("bac@example.invalid")
		bac.qualification, bac.program = "BAC_D", "IA"
		bac.evidence = func(row int) map[string]any {
			rows := []map[string]any{{}, {}, {}}
			source := "DIRECT_CELL"
			if row == 1 {
				source = "POINTS_TOTALS"
				rows[1] = map[string]any{"reconstructed": []string{"scale"}}
			}
			return map[string]any{"validation": "CELL_ARITHMETIC", "gradeSource": source, "arithmeticRowIndex": row, "arithmeticRows": rows, "series": "", "system": "NORMALIZED_TOP_LEFT"}
		}
		hash, d := h.prepare(bac)
		if _, _, err := h.subs.Submit(ctx, hash, d.Version); err != nil {
			t.Fatal(err)
		}
		rows, err := h.pool.Query(ctx, `SELECT s.validation_method,array_to_string(s.reconstructed_cells,',') FROM application_submission_subjects s JOIN application_submissions x ON x.id=s.submission_id WHERE x.application_id=$1::uuid ORDER BY s.position`, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for rows.Next() {
			var method, cells string
			rows.Scan(&method, &cells)
			got = append(got, method+":"+cells)
		}
		if strings.Join(got, " ") != "ROW_ARITHMETIC: TABLE_TOTALS:scale ROW_ARITHMETIC:" {
			t.Fatal(got)
		}
		if h.count(`SELECT count(*) FROM application_submission_flags WHERE application_id=$1::uuid AND code='SERIES_UNREADABLE'`, d.ID) != 1 {
			t.Fatal("unread series not flagged")
		}
		read := bac
		read.email = "bac-read@example.invalid"
		read.phone = "+22997000001"
		read.transcriptSHA = strings.Repeat("c", 64)
		read.evidence = func(row int) map[string]any {
			return map[string]any{"validation": "CELL_ARITHMETIC", "gradeSource": "DIRECT_CELL", "arithmeticRowIndex": row, "arithmeticRows": []map[string]any{{}, {}, {}}, "series": "D", "system": "NORMALIZED_TOP_LEFT"}
		}
		hash, d = h.prepare(read)
		if _, _, err := h.subs.Submit(ctx, hash, d.Version); err != nil {
			t.Fatal(err)
		}
		if h.count(`SELECT count(*) FROM application_submission_flags WHERE application_id=$1::uuid`, d.ID) != 0 {
			t.Fatal("read series flagged")
		}
	})

	t.Run("duplicates are flagged against submitted applications only, never blocking", func(t *testing.T) {
		first := h.person("twin@example.invalid")
		first.transcriptSHA = strings.Repeat("d", 64)
		hashA, a := h.prepare(first)
		second := h.person("TWIN@example.invalid")
		second.transcriptSHA = first.transcriptSHA
		hashB, b := h.prepare(second)
		third := h.person("other@example.invalid")
		third.phone = first.phone
		hashC, c := h.prepare(third) // Shares A's phone but stays a draft until the end.
		for _, s := range []struct {
			hash    []byte
			version int64
		}{{hashA, a.Version}, {hashB, b.Version}} {
			if _, _, err := h.subs.Submit(ctx, s.hash, s.version); err != nil {
				t.Fatal("duplicate blocked", err)
			}
		}
		flags := func(app string) string {
			rows, err := h.pool.Query(ctx, `SELECT code,coalesce(related_application_id::text,'') FROM application_submission_flags WHERE application_id=$1::uuid ORDER BY code`, app)
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for rows.Next() {
				var code, related string
				rows.Scan(&code, &related)
				if related == a.ID {
					related = "A"
				}
				out = append(out, code+">"+related)
			}
			return strings.Join(out, " ")
		}
		if got := flags(a.ID); got != "" {
			t.Fatal("first application flagged against a draft", got)
		}
		if got := flags(b.ID); got != "DUPLICATE_EMAIL>A DUPLICATE_TRANSCRIPT>A" {
			t.Fatal(got)
		}
		if _, _, err := h.subs.Submit(ctx, hashC, c.Version); err != nil {
			t.Fatal(err)
		}
		if got := flags(c.ID); got != "DUPLICATE_PHONE>A" {
			t.Fatal(got)
		}
	})

	t.Run("rollback of migration 00006 refused while submissions exist", func(t *testing.T) {
		if _, err := h.provider.Down(ctx); err == nil || !strings.Contains(err.Error(), "rollback refused") {
			t.Fatal("down accepted", err)
		}
		if h.count(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='applications' AND column_name='application_number'`) != 1 {
			t.Fatal("refused rollback changed the schema")
		}
	})
}

// same compares receipts as the client sees them, in JSON.
func same(a, b Receipt) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// concurrently starts n calls at the same instant on separate connections.
func concurrently(n int, call func(int) (Receipt, Outcome, error)) ([]Receipt, []Outcome, []error) {
	receipts, outcomes, errs := make([]Receipt, n), make([]Outcome, n), make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			receipts[i], outcomes[i], errs[i] = call(i)
		}()
	}
	close(start)
	wg.Wait()
	return receipts, outcomes, errs
}

func TestPostgresSubmissionMigrationRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h := isolated(t, ctx, 0, "")
	// A draft with an analysis in progress does not prevent the rollback.
	queued := h.person("roundtrip@example.invalid")
	queued.stopBefore = "analysis"
	hash, _ := h.prepare(queued)
	source := func() string {
		var body string
		if err := h.pool.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE proname='refresh_transcript_analysis' AND pronamespace=current_schema()::regnamespace`).Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if !strings.Contains(source(), "state='DRAFT') THEN RETURN") {
		t.Fatal("submitted analyses not protected")
	}
	if _, err := h.provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(source(), "state='DRAFT') THEN RETURN") || h.count(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('application_submissions','audit_events','application_number_counters')`) != 0 ||
		h.count(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND column_name IN ('numbering_year','application_number','submitted_at')`) != 0 {
		t.Fatal("rollback incomplete")
	}
	if _, err := h.provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if n := h.count(`SELECT numbering_year FROM admission_campaigns WHERE code='DEV_LOCAL'`); n != 2000 {
		t.Fatal("backfill after reapply", n)
	}
	if v, err := h.analyses.Current(ctx, hash); err != nil || v.Status != "QUEUED" {
		t.Fatal("draft analysis lost by the round trip", err)
	}
}
