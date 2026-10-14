package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ifri-admissions/services/api/internal/modules/audit"
)

// Synthetic names: common first and last names combined at random, never a real person's
// record; emails use the reserved example.invalid domain.
var (
	syntheticLast  = []string{"Adjovi", "Agossou", "Ahouandjinou", "Akpo", "Amoussou", "Assogba", "Avocè", "Azonhoumon", "Bello", "Chabi", "Dossou", "Gbaguidi", "Gbèdji", "Houngbédji", "Hounkpè", "Kpadonou", "Kossou", "Lawani", "Mêtonou", "Noudéhou", "Sossa", "Tossou", "Yèhouénou", "Zinsou"}
	syntheticFirst = []string{"Afiavi", "Akossiwa", "Aurélie", "Bénédicte", "Comlan", "Djidjoho", "Élodie", "Fifamè", "Gisèle", "Hermann", "Ifèdé", "Jérôme", "Kocou", "Léa", "Mahougnon", "Noël", "Océane", "Pélagie", "Rodrigue", "Sèna", "Sènan", "Ulrich", "Yénoukoumè", "Zoé", "Éric"}
)

// GenerateSynthetic inserts count submitted applications into a campaign, set-based and in one
// transaction, through every phase 6 constraint (snapshot, documents, analysis, numbers): the
// database checks them at commit exactly as for a real submission. Documents point to no stored
// object, so their previews and OCR proofs are reported unavailable. Development and test only.
func GenerateSynthetic(ctx context.Context, pool *pgxpool.Pool, campaign string, count int) (int, error) {
	if count < 1 || count > 20000 {
		return 0, errors.New("count must be between 1 and 20000")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ifri-synthetic',0))`); err != nil {
		return 0, err
	}
	var campaignID string
	var year, offset int
	if err = tx.QueryRow(ctx, `SELECT id::text,numbering_year FROM admission_campaigns WHERE code=$1`, campaign).Scan(&campaignID, &year); err != nil {
		return 0, fmt.Errorf("campaign %s not found", campaign)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM candidates WHERE email LIKE 'synthetic-%@example.invalid'`).Scan(&offset); err != nil {
		return 0, err
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`CREATE TEMP TABLE syn ON COMMIT DROP AS
		 WITH pairs AS (
		  SELECT row_number() OVER (ORDER BY p.code,q.code) AS rn,cp.id AS campaign_program_id,cp.program_id,p.code AS program_code,
		   q.id AS qualification_id,q.code AS qualification_code,v.id AS grid_id,e.id AS eligibility_id
		  FROM campaign_programs cp JOIN programs p ON p.id=cp.program_id AND p.enabled
		  JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.status='PUBLISHED'
		  JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id AND e.eligible
		  JOIN origin_qualifications q ON q.id=e.origin_qualification_id AND q.enabled
		  WHERE cp.campaign_id=$1::uuid AND cp.enabled), total AS (SELECT count(*) AS n FROM pairs)
		 SELECT g+$2 AS n,gen_random_uuid() AS candidate_id,gen_random_uuid() AS application_id,
		  ($4::text[])[1+((g*7)%cardinality($4::text[]))] AS last_name,($5::text[])[1+((g*13)%cardinality($5::text[]))] AS first_names,
		  'synthetic-'||(CASE WHEN (g+$2)%50=0 THEN g+$2-1 ELSE g+$2 END)||'@example.invalid' AS email,
		  '+22990'||lpad((CASE WHEN (g+$2)%70=0 THEN g+$2-1 ELSE g+$2 END)::text,8,'0') AS phone,
		  now()-(((g+$2)%720)*interval '1 hour')-((g%60)*interval '1 minute') AS submitted_at,pairs.*
		 FROM generate_series(1,$3) g CROSS JOIN total JOIN pairs ON pairs.rn=1+(g%total.n)`,
			[]any{campaignID, offset, count, syntheticLast, syntheticFirst}},
		{`INSERT INTO candidates(id,last_name,first_names,email,phone) SELECT candidate_id,last_name,first_names,email,phone FROM syn`, nil},
		{`INSERT INTO applications(id,candidate_id,campaign_id,campaign_program_id,origin_qualification_id,graduation_year)
		 SELECT application_id,candidate_id,$1::uuid,campaign_program_id,qualification_id,2025 FROM syn`, []any{campaignID}},
		{`CREATE TEMP TABLE syn_docs ON COMMIT DROP AS SELECT s.n,s.application_id,t.type,gen_random_uuid() AS document_id,gen_random_uuid() AS version_id,
		  encode(sha256(convert_to(CASE WHEN t.type='TRANSCRIPT' THEN 'synthetic-transcript-'||(CASE WHEN s.n%90=0 THEN s.n-1 ELSE s.n END)
		   ELSE 'synthetic-'||t.type||'-'||s.n END,'UTF8')),'hex') AS sha
		 FROM syn s CROSS JOIN unnest(ARRAY['REQUEST_LETTER','TRANSCRIPT','IDENTITY_DOCUMENT','PARENTAL_COMMITMENT']) t(type)`, nil},
		{`INSERT INTO application_documents(id,application_id,document_type,current_version_id) SELECT document_id,application_id,type,version_id FROM syn_docs`, nil},
		{`INSERT INTO document_versions(id,application_document_id,version_number,storage_key,original_filename,content_type,size_bytes,sha256)
		 SELECT version_id,document_id,1,'applications/'||application_id||'/documents/'||type||'/'||version_id||'.pdf','document-synthetique.pdf','application/pdf',1000,sha FROM syn_docs`, nil},
		{`CREATE TEMP TABLE syn_analyses ON COMMIT DROP AS SELECT s.application_id,gen_random_uuid() AS analysis_id,d.version_id,s.grid_id,s.program_id,s.qualification_id,
		  (SELECT jsonb_agg(jsonb_build_object('id',sub.id,'code',sub.code,'name',sub.name,'position',r.position) ORDER BY r.position)
		   FROM expected_subject_rules r JOIN subjects sub ON sub.id=r.subject_id WHERE r.eligibility_id=s.eligibility_id) AS expected
		 FROM syn s JOIN syn_docs d ON d.application_id=s.application_id AND d.type='TRANSCRIPT'`, nil},
		{`INSERT INTO ocr_analyses(id,application_id,document_version_id,rule_grid_version_id,program_id,origin_qualification_id,expected_subjects,status,engine,engine_version,attempt_number,started_at,completed_at)
		 SELECT analysis_id,application_id,version_id,grid_id,program_id,qualification_id,expected,'SUCCEEDED','synthetic','1',1,now(),now() FROM syn_analyses`, nil},
		{`INSERT INTO ocr_extracted_subjects(ocr_analysis_id,subject_id,position,detected_subject_text,grade_raw,grade_value,coefficient_raw,coefficient_value,page_number,
		  subject_confidence,grade_confidence,coefficient_confidence,overall_confidence,evidence)
		 SELECT a.analysis_id,(e->>'id')::uuid,(e->>'position')::int,e->>'name',v.grade::text,v.grade,v.coef::text,v.coef,1,1,1,1,1,
		  jsonb_build_object('system','NORMALIZED_TOP_LEFT',
		   'subject',jsonb_build_object('x0',0.08,'y0',v.y,'x1',0.45,'y1',v.y+0.03),
		   'grade',jsonb_build_object('x0',0.5,'y0',v.y,'x1',0.6,'y1',v.y+0.03),
		   'coefficient',jsonb_build_object('x0',0.62,'y0',v.y,'x1',0.7,'y1',v.y+0.03))
		 FROM syn_analyses a CROSS JOIN LATERAL jsonb_array_elements(a.expected) e
		 CROSS JOIN LATERAL (SELECT round((800+abs(hashtext(a.analysis_id::text||(e->>'position')))%1000)::numeric/100,2) AS grade,
		  (1+abs(hashtext(a.analysis_id::text||'c'||(e->>'position')))%4)::numeric AS coef,
		  round(0.3+(e->>'position')::numeric*0.05,3) AS y) v`, nil},
		{`UPDATE ocr_analyses SET candidate_confirmation='CONFIRMED',confirmed_at=now(),result_digest=encode(sha256(convert_to(id::text,'UTF8')),'hex')
		 WHERE id IN (SELECT analysis_id FROM syn_analyses)`, nil},
		{`INSERT INTO application_submissions(application_id,campaign_id,campaign_program_id,program_id,origin_qualification_id,graduation_year,last_name,first_names,email,phone,
		  validation_path,rule_set_version_id,ocr_analysis_id,transcript_version_id,transcript_sha256,weighted_sum,coefficient_sum,average,average_is_exact,calculation_version,created_at)
		 SELECT s.application_id,$1::uuid,s.campaign_program_id,s.program_id,s.qualification_id,2025,s.last_name,s.first_names,s.email,s.phone,'CANDIDATE_CONFIRMED_OCR',s.grid_id,
		  a.analysis_id,d.version_id,d.sha,m.weighted,m.coefficients,
		  div(m.weighted*1e30,m.coefficients)*0.000000000000000000000000000001,
		  div(m.weighted*1e30,m.coefficients)*0.000000000000000000000000000001*m.coefficients=m.weighted,1,s.submitted_at
		 FROM syn s JOIN syn_analyses a ON a.application_id=s.application_id JOIN syn_docs d ON d.application_id=s.application_id AND d.type='TRANSCRIPT'
		 JOIN (SELECT a2.application_id,sum(x.grade_value*x.coefficient_value) AS weighted,sum(x.coefficient_value) AS coefficients
		  FROM syn_analyses a2 JOIN ocr_extracted_subjects x ON x.ocr_analysis_id=a2.analysis_id GROUP BY a2.application_id) m ON m.application_id=s.application_id`, []any{campaignID}},
		{`INSERT INTO application_submission_subjects(submission_id,ocr_extracted_subject_id,subject_id,subject_code,subject_name,position,grade_raw,grade_value,coefficient_raw,coefficient_value,validation_method,reconstructed_cells)
		 SELECT sub.id,x.id,x.subject_id,sj.code,sj.name,x.position,x.grade_raw,x.grade_value,x.coefficient_raw,x.coefficient_value,'DIRECT_READ','{}'
		 FROM application_submissions sub JOIN syn_analyses a ON a.application_id=sub.application_id JOIN ocr_extracted_subjects x ON x.ocr_analysis_id=a.analysis_id JOIN subjects sj ON sj.id=x.subject_id`, nil},
		{`INSERT INTO application_submission_documents(submission_id,document_type,document_version_id,sha256)
		 SELECT sub.id,d.type,d.version_id,d.sha FROM application_submissions sub JOIN syn_docs d ON d.application_id=sub.application_id`, nil},
		// Same signals as a real submission: duplicates point to the previous dossier of the pair.
		{`INSERT INTO application_submission_flags(application_id,code,related_application_id)
		 SELECT s.application_id,f.code,p.application_id FROM syn s JOIN syn p ON p.n=s.n-1
		 CROSS JOIN LATERAL (VALUES ('DUPLICATE_EMAIL',s.n%50=0),('DUPLICATE_PHONE',s.n%70=0),('DUPLICATE_TRANSCRIPT',s.n%90=0)) f(code,applies) WHERE f.applies`, nil},
		{`INSERT INTO application_submission_flags(application_id,code) SELECT application_id,'SERIES_UNREADABLE' FROM syn WHERE qualification_code LIKE 'BAC\_%'`, nil},
	}
	for i, step := range steps {
		if _, err = tx.Exec(ctx, step.sql, step.args...); err != nil {
			return 0, fmt.Errorf("synthetic step %d: %w", i+1, err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT application_id::text,program_id::text,program_code,submitted_at FROM syn ORDER BY submitted_at,n`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id, program, code string
		at                time.Time
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
		var p pending
		return p, row.Scan(&p.id, &p.program, &p.code, &p.at)
	})
	if err != nil {
		return 0, err
	}
	// Numbers through the real counter, one increment at a time (the counter only moves by 1).
	batch := &pgx.Batch{}
	for _, p := range list {
		batch.Queue(`INSERT INTO application_number_counters(numbering_year,program_id,last_serial) VALUES($1,$2::uuid,1)
		 ON CONFLICT (numbering_year,program_id) DO UPDATE SET last_serial=application_number_counters.last_serial+1 RETURNING last_serial`, year, p.program)
	}
	results := tx.SendBatch(ctx, batch)
	ids, numbers, dates := make([]string, len(list)), make([]string, len(list)), make([]time.Time, len(list))
	for i, p := range list {
		var serial int
		if err = results.QueryRow().Scan(&serial); err != nil {
			results.Close()
			return 0, err
		}
		ids[i], numbers[i], dates[i] = p.id, fmt.Sprintf("IFRI-%04d-%s-%06d", year, p.code, serial), p.at
	}
	if err = results.Close(); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE applications a SET state='SUBMITTED',application_number=v.number,submitted_at=v.at,version=a.version+1,updated_at=now()
	 FROM unnest($1::uuid[],$2::text[],$3::timestamptz[]) v(id,number,at) WHERE a.id=v.id`, ids, numbers, dates); err != nil {
		return 0, err
	}
	if err = audit.Record(ctx, tx, "SYNTHETIC_DATA_GENERATED", audit.Actor{Type: audit.ActorSystem}, "", "", map[string]any{"campaign": campaign, "count": len(list)}); err != nil {
		return 0, err
	}
	return len(list), tx.Commit(ctx)
}
