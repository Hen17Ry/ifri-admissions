//go:build integration

package references

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

func TestPostgresReferences(t *testing.T) {
	c, err := config.Load()
	if err != nil {
		t.Fatal("Integration tests require project environment:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal("integration database configuration failed")
	}
	t.Cleanup(admin.Close)
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	schema := "ifri_test_" + hex.EncodeToString(token)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("integration database unavailable:", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("isolated schema cleanup failed:", err)
		}
	})
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pc.MaxConns = 3
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var db *sql.DB = stdlib.OpenDB(*pc.ConnConfig)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("migrate empty schema", func(t *testing.T) {
		if _, err = provider.Up(ctx); err != nil {
			t.Fatal(err)
		}
	})
	if t.Failed() {
		return
	}
	t.Run("seed twice preserves rows and IDs", func(t *testing.T) {
		if err = seeds.Development(ctx, pool, "test"); err != nil {
			t.Fatal(err)
		}
		snapshot := func() map[string]string {
			out := map[string]string{}
			for _, table := range []string{"admission_campaigns", "programs", "origin_qualifications", "subjects", "campaign_programs", "rule_set_versions", "program_qualification_eligibilities", "expected_subject_rules"} {
				var value string
				if err := pool.QueryRow(ctx, "SELECT coalesce(json_agg(t ORDER BY id)::text,'[]') FROM "+table+" t").Scan(&value); err != nil {
					t.Fatal(err)
				}
				out[table] = value
			}
			return out
		}
		before := snapshot()
		if err = seeds.Development(ctx, pool, "test"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("seed changed existing rows")
		}
		if err = seeds.Development(ctx, pool, "production"); err == nil {
			t.Fatal("production seed was allowed")
		}
	})
	if t.Failed() {
		return
	}
	service := NewService(NewRepository(pool))
	cases := []struct {
		program, qualification string
		subjects               []string
	}{
		{"GL", "BAC_C", []string{"MATH", "FRENCH", "ENGLISH"}},
		{"GL", "BAC_D", []string{"MATH", "FRENCH", "ENGLISH"}},
		{"GL", "BAC_E", []string{"MATH", "FRENCH", "MANUFACTURING_STUDY"}},
		{"GL", "DT_IMI", []string{"APPLIED_MATH", "FRENCH", "INFORMATION_SYSTEMS_TECH"}},
		{"GL", "DT_DWM", []string{"APPLIED_MATH", "FRENCH", "WEB_SITES_APPS"}},
		{"IM", "DT_PM", []string{"IT_PROJECT", "FRENCH", "EDITING"}},
		{"SEIOT", "DT_EAP", []string{"APPLIED_SCIENCES", "FRENCH", "ELECTRONICS_STUDY"}},
		{"SI", "DT_IMI", []string{"APPLIED_MATH", "FRENCH", "INFORMATION_SYSTEMS_TECH"}},
	}
	t.Run("required business matrix", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.program+"_"+tc.qualification, func(t *testing.T) {
				out, err := service.Resolve(ctx, "DEV_LOCAL", tc.program, tc.qualification)
				if err != nil {
					t.Fatal(err)
				}
				codes := []string{}
				for _, subject := range out.ExpectedSubjects {
					codes = append(codes, subject.Code)
				}
				if !out.Eligible || !reflect.DeepEqual(codes, tc.subjects) {
					t.Fatalf("unexpected resolution %+v", out)
				}
			})
		}
	})
	t.Run("entire matrix and BAC E across programs", func(t *testing.T) {
		allowed := map[string][]string{"IA": {"BAC_C", "BAC_D", "BAC_E"}, "SI": {"BAC_C", "BAC_D", "BAC_E", "DT_IMI"}, "GL": {"BAC_C", "BAC_D", "BAC_E", "DT_IMI", "DT_DWM"}, "IM": {"BAC_C", "BAC_D", "BAC_E", "DT_IMI", "DT_DWM", "DT_PM"}, "SEIOT": {"BAC_C", "BAC_D", "BAC_E", "DT_IMI", "DT_EAP"}}
		for program, qualifications := range allowed {
			for _, qualification := range []string{"BAC_C", "BAC_D", "BAC_E", "DT_IMI", "DT_DWM", "DT_PM", "DT_EAP"} {
				out, err := service.Resolve(ctx, "DEV_LOCAL", program, qualification)
				if err != nil {
					t.Fatal(err)
				}
				want := false
				for _, q := range qualifications {
					want = want || q == qualification
				}
				if out.Eligible != want {
					t.Fatalf("%s/%s eligibility", program, qualification)
				}
				if want && qualification == "BAC_E" && out.ExpectedSubjects[2].Code != "MANUFACTURING_STUDY" {
					t.Fatal("BAC E must use manufacturing study")
				}
				if !want && (out.Reason != "RULE_NOT_DEFINED" || len(out.ExpectedSubjects) != 0) {
					t.Fatal("unknown rule silently authorized")
				}
			}
		}
	})
	t.Run("explicit refusal and campaign isolation", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO admission_campaigns(code,name,academic_year,numbering_year,starts_at,ends_at,status) VALUES ('TEST_OTHER','Test only','Test',2000,'2000-01-01','2100-01-01','OPEN');
  INSERT INTO campaign_programs(campaign_id,program_id,available_seats) SELECT c.id,p.id,1 FROM admission_campaigns c CROSS JOIN programs p WHERE c.code='TEST_OTHER' AND p.code='GL';
  INSERT INTO rule_set_versions(campaign_program_id,version,source,status) SELECT cp.id,1,'Test fixture','DRAFT' FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id WHERE c.code='TEST_OTHER';
  INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible) SELECT v.id,q.id,q.code='BAC_C' FROM rule_set_versions v JOIN campaign_programs cp ON cp.id=v.campaign_program_id JOIN admission_campaigns c ON c.id=cp.campaign_id CROSS JOIN origin_qualifications q WHERE c.code='TEST_OTHER' AND q.code IN ('BAC_C','DT_DWM');
  INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT e.id,s.id,1 FROM program_qualification_eligibilities e JOIN rule_set_versions v ON v.id=e.rule_set_version_id JOIN campaign_programs cp ON cp.id=v.campaign_program_id JOIN admission_campaigns c ON c.id=cp.campaign_id CROSS JOIN subjects s WHERE c.code='TEST_OTHER' AND e.eligible AND s.code='MATH';
  UPDATE rule_set_versions SET status='PUBLISHED' WHERE campaign_program_id IN (SELECT cp.id FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id WHERE c.code='TEST_OTHER');`)
		if err != nil {
			t.Fatal(err)
		}
		refused, err := service.Resolve(ctx, "TEST_OTHER", "GL", "DT_DWM")
		if err != nil || refused.Eligible || refused.Reason != "INELIGIBLE" {
			t.Fatalf("explicit refusal: %+v, %v", refused, err)
		}
		other, err := service.Resolve(ctx, "TEST_OTHER", "GL", "BAC_C")
		if err != nil || len(other.ExpectedSubjects) != 1 {
			t.Fatalf("campaign isolation: %+v, %v", other, err)
		}
		if _, err = service.Campaign(ctx, ""); !errors.Is(err, ErrAmbiguousCampaign) {
			t.Fatalf("multiple open campaigns must require selection: %v", err)
		}
		offered, err := service.Programs(ctx, "TEST_OTHER", "")
		// An explicit quota is exposed as such, never confused with an unset one (since phase 9 a
		// quota lies between 1 and 10,000).
		if err != nil || len(offered) != 1 || offered[0].AvailableSeats == nil || *offered[0].AvailableSeats != 1 {
			t.Fatalf("explicit quota: %+v, %v", offered, err)
		}
	})

	t.Run("child trigger branches on draft rows", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var version, eligibility string
		if err = tx.QueryRow(ctx, `INSERT INTO rule_set_versions(campaign_program_id,version,source,status) SELECT id,2,'Trigger regression','DRAFT' FROM campaign_programs LIMIT 1 RETURNING id::text`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(ctx, `INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible) SELECT $1::uuid,id,true FROM origin_qualifications WHERE code='BAC_C' RETURNING id::text`, version).Scan(&eligibility); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`UPDATE program_qualification_eligibilities SET eligible=true WHERE id=$1::uuid`,
			`INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT $1::uuid,id,1 FROM subjects WHERE code='MATH'`,
			`UPDATE expected_subject_rules SET position=1 WHERE eligibility_id=$1::uuid`,
			`DELETE FROM expected_subject_rules WHERE eligibility_id=$1::uuid`,
			`DELETE FROM program_qualification_eligibilities WHERE id=$1::uuid`,
		} {
			if _, err = tx.Exec(ctx, statement, eligibility); err != nil {
				t.Fatalf("trigger regression: %v", err)
			}
		}
	})

	t.Run("database constraints", func(t *testing.T) {
		checks := []struct{ name, statement, code string }{
			{"duplicate program", `INSERT INTO programs(code,name) VALUES ('GL','Duplicate')`, "23505"},
			{"invalid dates", `INSERT INTO admission_campaigns(code,name,academic_year,numbering_year,starts_at,ends_at,status) VALUES ('BAD_DATE','Test','Test',2026,now(),now(),'OPEN')`, "23514"},
			{"invalid campaign status", `UPDATE admission_campaigns SET status='ACTIVE' WHERE code='DEV_LOCAL'`, "23514"},
			{"negative quota", `UPDATE campaign_programs SET available_seats=-1`, "23514"},
			{"duplicate offer", `INSERT INTO campaign_programs(campaign_id,program_id) SELECT campaign_id,program_id FROM campaign_programs LIMIT 1`, "23505"},
			{"broken relation", `INSERT INTO campaign_programs(campaign_id,program_id) VALUES(gen_random_uuid(),gen_random_uuid())`, "23503"},
			{"published immutable", `UPDATE expected_subject_rules SET position=position`, "23514"},
			{"publication cannot bypass guard", `INSERT INTO rule_set_versions(campaign_program_id,version,source,status) SELECT id,99,'Test','PUBLISHED' FROM campaign_programs LIMIT 1`, "23514"},
		}
		for _, check := range checks {
			t.Run(check.name, func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				_, err = tx.Exec(ctx, check.statement)
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != check.code {
					t.Fatalf("wanted %s, got %v", check.code, err)
				}
			})
		}
		for _, check := range []struct{ name, statement, code string }{
			{"duplicate qualification", `INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible) SELECT rule_set_version_id,origin_qualification_id,true FROM program_qualification_eligibilities WHERE id=$1::uuid`, "23505"},
			{"duplicate subject", `INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT $1::uuid,id,2 FROM subjects WHERE code='MATH'`, "23505"},
			{"duplicate position", `INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT $1::uuid,id,1 FROM subjects WHERE code='FRENCH'`, "23505"},
			{"zero position", `INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT $1::uuid,id,0 FROM subjects WHERE code='FRENCH'`, "23514"},
		} {
			t.Run(check.name, func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				var version, eligibility string
				if err = tx.QueryRow(ctx, `INSERT INTO rule_set_versions(campaign_program_id,version,source,status) SELECT id,2,'Test','DRAFT' FROM campaign_programs LIMIT 1 RETURNING id::text`).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if err = tx.QueryRow(ctx, `INSERT INTO program_qualification_eligibilities(rule_set_version_id,origin_qualification_id,eligible) SELECT $1::uuid,id,true FROM origin_qualifications WHERE code='BAC_C' RETURNING id::text`, version).Scan(&eligibility); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(ctx, `INSERT INTO expected_subject_rules(eligibility_id,subject_id,position) SELECT $1::uuid,id,1 FROM subjects WHERE code='MATH'`, eligibility); err != nil {
					t.Fatal(err)
				}
				_, err = tx.Exec(ctx, check.statement, eligibility)
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != check.code {
					t.Fatalf("wanted %s, got %v", check.code, err)
				}
			})
		}
		var columns int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='expected_subject_rules' AND column_name IN ('coefficient','subject_coefficient','default_coefficient','configured_coefficient','weight','note','grade')`, schema).Scan(&columns); err != nil || columns != 0 {
			t.Fatalf("forbidden fields: %d, %v", columns, err)
		}
	})
	t.Run("HTTP with real repository", func(t *testing.T) {
		mux := http.NewServeMux()
		Register(mux, service, slog.New(slog.NewTextHandler(io.Discard, nil)))
		for _, tc := range cases {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/expected-subjects?campaign=DEV_LOCAL&program="+tc.program+"&qualification="+tc.qualification, nil))
			if w.Code != 200 {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body)
			}
			var out Resolution
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !out.Eligible {
				t.Fatal("invalid HTTP result")
			}
		}
	})
	t.Run("rollback and reapply", func(t *testing.T) {
		if _, err = provider.DownTo(ctx, 0); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name='admission_campaigns'`, schema).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback failed: %v", err)
		}
		if _, err = provider.Up(ctx); err != nil {
			t.Fatal(err)
		}
		if err = seeds.Development(ctx, pool, "test"); err != nil {
			t.Fatal(err)
		}
	})
}
