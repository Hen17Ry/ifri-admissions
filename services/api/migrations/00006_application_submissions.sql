-- +goose Up
-- Phase 6: final submission, application number, immutable snapshot, admin flags, audit.
-- The number year is a campaign attribute: a campaign spanning two calendar years keeps one year.
ALTER TABLE admission_campaigns ADD COLUMN numbering_year smallint;
UPDATE admission_campaigns SET numbering_year=extract(year FROM starts_at AT TIME ZONE 'Africa/Porto-Novo')::smallint;
ALTER TABLE admission_campaigns ALTER COLUMN numbering_year SET NOT NULL,
 ADD CONSTRAINT campaign_numbering_year CHECK (numbering_year BETWEEN 2000 AND 9999);

ALTER TABLE applications
 ADD COLUMN application_number text UNIQUE CHECK (application_number ~ '^IFRI-[0-9]{4}-[A-Z][A-Z0-9_]{0,63}-[0-9]{6}$'),
 ADD COLUMN submitted_at timestamptz,
 ADD CONSTRAINT application_draft_unnumbered CHECK (state<>'DRAFT' OR (application_number IS NULL AND submitted_at IS NULL)),
 ADD CONSTRAINT application_submitted_numbered CHECK (state<>'SUBMITTED' OR (application_number IS NOT NULL AND submitted_at IS NOT NULL));

-- One counter row per (numbering year, program), incremented inside the submission transaction.
CREATE TABLE application_number_counters (
 numbering_year smallint NOT NULL,
 program_id uuid NOT NULL REFERENCES programs(id),
 last_serial integer NOT NULL CHECK (last_serial BETWEEN 1 AND 999999),
 PRIMARY KEY (numbering_year,program_id)
);

-- validation_path is extensible: a later MANUAL_REVIEW_REQUIRED path only widens the CHECKs below.
CREATE TABLE application_submissions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL UNIQUE REFERENCES applications(id),
 campaign_id uuid NOT NULL REFERENCES admission_campaigns(id),
 campaign_program_id uuid NOT NULL REFERENCES campaign_programs(id),
 program_id uuid NOT NULL REFERENCES programs(id),
 origin_qualification_id uuid NOT NULL REFERENCES origin_qualifications(id),
 graduation_year integer NOT NULL CHECK (graduation_year BETWEEN 1900 AND 9999),
 last_name text NOT NULL CHECK (btrim(last_name)<>'' AND length(last_name)<=100),
 first_names text NOT NULL CHECK (btrim(first_names)<>'' AND length(first_names)<=150),
 email text NOT NULL CHECK (email<>'' AND length(email)<=254 AND email=lower(btrim(email))),
 phone text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{6,14}$'),
 validation_path text NOT NULL CHECK (validation_path IN ('CANDIDATE_CONFIRMED_OCR')),
 rule_set_version_id uuid NOT NULL REFERENCES rule_set_versions(id),
 ocr_analysis_id uuid UNIQUE REFERENCES ocr_analyses(id),
 transcript_version_id uuid NOT NULL REFERENCES document_versions(id),
 transcript_sha256 text NOT NULL CHECK (transcript_sha256 ~ '^[a-f0-9]{64}$'),
 -- Exact sums of the copied subjects; the fraction weighted_sum / coefficient_sum is authoritative.
 weighted_sum numeric CHECK (weighted_sum>=0),
 coefficient_sum numeric CHECK (coefficient_sum>0),
 -- Exact when the decimal expansion terminates within 30 places, otherwise truncated (never rounded).
 average numeric(32,30) CHECK (average BETWEEN 0 AND 20),
 average_is_exact boolean,
 calculation_version integer CHECK (calculation_version=1),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (validation_path<>'CANDIDATE_CONFIRMED_OCR' OR (ocr_analysis_id IS NOT NULL AND weighted_sum IS NOT NULL AND coefficient_sum IS NOT NULL AND average IS NOT NULL AND average_is_exact IS NOT NULL AND calculation_version IS NOT NULL)),
 CHECK ((weighted_sum IS NULL)=(average IS NULL) AND (coefficient_sum IS NULL)=(average IS NULL)),
 CHECK (average IS NULL OR (
  average*coefficient_sum<=weighted_sum
  AND weighted_sum<(average+0.000000000000000000000000000001)*coefficient_sum
  AND average_is_exact=(average*coefficient_sum=weighted_sum)
 ))
);
CREATE INDEX submissions_campaign_email ON application_submissions(campaign_id,email);
CREATE INDEX submissions_campaign_phone ON application_submissions(campaign_id,phone);
CREATE INDEX submissions_campaign_transcript ON application_submissions(campaign_id,transcript_sha256);

CREATE TABLE application_submission_subjects (
 submission_id uuid NOT NULL REFERENCES application_submissions(id),
 ocr_extracted_subject_id uuid NOT NULL UNIQUE REFERENCES ocr_extracted_subjects(id),
 subject_id uuid NOT NULL REFERENCES subjects(id),
 subject_code text NOT NULL,
 subject_name text NOT NULL,
 position integer NOT NULL CHECK (position>0),
 grade_raw text NOT NULL CHECK (length(grade_raw)<=32),
 grade_value numeric NOT NULL CHECK (grade_value>=0 AND grade_value<=20),
 coefficient_raw text NOT NULL CHECK (length(coefficient_raw)<=32),
 coefficient_value numeric NOT NULL CHECK (coefficient_value>0 AND coefficient_value NOT IN ('NaN'::numeric,'Infinity'::numeric,'-Infinity'::numeric)),
 -- DIRECT_READ: generic path; ROW_ARITHMETIC: note read and Points = Note x Coeff on its row;
 -- TABLE_TOTALS: unreadable note derived as Points / Coeff, proven by the table totals.
 validation_method text NOT NULL CHECK (validation_method IN ('DIRECT_READ','ROW_ARITHMETIC','TABLE_TOTALS')),
 reconstructed_cells text[] NOT NULL DEFAULT '{}' CHECK (reconstructed_cells <@ ARRAY['points','scale']::text[]),
 PRIMARY KEY (submission_id,subject_id),
 UNIQUE (submission_id,position)
);

CREATE TABLE application_submission_documents (
 submission_id uuid NOT NULL REFERENCES application_submissions(id),
 document_type text NOT NULL CHECK (document_type IN ('REQUEST_LETTER','TRANSCRIPT','IDENTITY_DOCUMENT','PARENTAL_COMMITMENT')),
 document_version_id uuid NOT NULL UNIQUE REFERENCES document_versions(id),
 sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
 PRIMARY KEY (submission_id,document_type)
);

-- Non-blocking signals for the administration; the first application of a pair carries none.
CREATE TABLE application_submission_flags (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES application_submissions(application_id),
 code text NOT NULL CHECK (code IN ('DUPLICATE_EMAIL','DUPLICATE_PHONE','DUPLICATE_TRANSCRIPT','SERIES_UNREADABLE')),
 related_application_id uuid REFERENCES application_submissions(application_id),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((code='SERIES_UNREADABLE')=(related_application_id IS NULL)),
 CHECK (related_application_id IS DISTINCT FROM application_id),
 UNIQUE NULLS NOT DISTINCT (application_id,code,related_application_id)
);
CREATE INDEX submission_flags_related ON application_submission_flags(related_application_id) WHERE related_application_id IS NOT NULL;

-- Generic append-only journal, reused by the administration (phase 7). Technical metadata only.
CREATE TABLE audit_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 event_type text NOT NULL CHECK (event_type ~ '^[A-Z][A-Z0-9_]{0,63}$'),
 actor_type text NOT NULL CHECK (actor_type IN ('CANDIDATE','ADMIN','SYSTEM')),
 actor_id uuid,
 target_type text CHECK (target_type ~ '^[A-Z][A-Z0-9_]{0,63}$'),
 target_id uuid,
 metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata)='object'),
 occurred_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((target_type IS NULL)=(target_id IS NULL))
);
CREATE INDEX audit_events_target ON audit_events(target_type,target_id,occurred_at);

-- +goose StatementBegin
CREATE FUNCTION guard_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER submissions_immutable BEFORE UPDATE OR DELETE ON application_submissions FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_subjects_immutable BEFORE UPDATE OR DELETE ON application_submission_subjects FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_documents_immutable BEFORE UPDATE OR DELETE ON application_submission_documents FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_flags_immutable BEFORE UPDATE OR DELETE ON application_submission_flags FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER audit_events_immutable BEFORE UPDATE OR DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submissions_no_truncate BEFORE TRUNCATE ON application_submissions FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_subjects_no_truncate BEFORE TRUNCATE ON application_submission_subjects FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_documents_no_truncate BEFORE TRUNCATE ON application_submission_documents FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER submission_flags_no_truncate BEFORE TRUNCATE ON application_submission_flags FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER number_counters_no_truncate BEFORE TRUNCATE ON application_number_counters FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();

-- A counter only moves forward by one: a number is never reissued.
CREATE FUNCTION guard_number_counter() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.numbering_year<>OLD.numbering_year OR NEW.program_id<>OLD.program_id OR NEW.last_serial<>OLD.last_serial+1 THEN
  RAISE EXCEPTION 'Application number counters only increase' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER number_counter_guard BEFORE UPDATE OR DELETE ON application_number_counters FOR EACH ROW EXECUTE FUNCTION guard_number_counter();

-- Only a DRAFT row changes; SUBMITTED and CANCELLED rows stay as written. Administrative review
-- and selection statuses live in separate history tables, never in this row.
CREATE FUNCTION guard_locked_application() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>'DRAFT' THEN RAISE EXCEPTION 'Submitted application is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER application_locked BEFORE UPDATE OR DELETE ON applications FOR EACH ROW EXECUTE FUNCTION guard_locked_application();

CREATE FUNCTION guard_locked_candidate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM applications WHERE candidate_id=OLD.id AND state<>'DRAFT') THEN
  RAISE EXCEPTION 'Candidate of a submitted application is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER candidate_locked BEFORE UPDATE OR DELETE ON candidates FOR EACH ROW EXECUTE FUNCTION guard_locked_candidate();

CREATE FUNCTION guard_locked_documents() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app uuid; document_id uuid;
BEGIN
 IF TG_TABLE_NAME='application_documents' THEN
  IF TG_OP='DELETE' THEN app:=OLD.application_id; ELSE app:=NEW.application_id; END IF;
  -- Documents never move to another application.
  IF TG_OP='UPDATE' AND OLD.application_id<>NEW.application_id THEN app:=NULL; END IF;
 ELSE
  IF TG_OP='DELETE' THEN document_id:=OLD.application_document_id; ELSE document_id:=NEW.application_document_id; END IF;
  SELECT application_id INTO app FROM application_documents WHERE id=document_id;
 END IF;
 IF app IS NULL OR EXISTS (SELECT 1 FROM applications WHERE id=app AND state<>'DRAFT') THEN
  RAISE EXCEPTION 'Documents of a submitted application are immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER documents_locked BEFORE INSERT OR UPDATE OR DELETE ON application_documents FOR EACH ROW EXECUTE FUNCTION guard_locked_documents();
CREATE TRIGGER document_versions_locked BEFORE INSERT OR UPDATE OR DELETE ON document_versions FOR EACH ROW EXECUTE FUNCTION guard_locked_documents();

-- Extracted subjects are written once, before the result digest; never after a candidate decision.
CREATE FUNCTION guard_ocr_subject_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM ocr_analyses WHERE id=NEW.ocr_analysis_id AND (result_digest IS NOT NULL OR candidate_confirmation<>'PENDING')) THEN
  RAISE EXCEPTION 'OCR evidence is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER ocr_subjects_insert_closed BEFORE INSERT ON ocr_extracted_subjects FOR EACH ROW EXECUTE FUNCTION guard_ocr_subject_insert();
CREATE TRIGGER ocr_subjects_no_truncate BEFORE TRUNCATE ON ocr_extracted_subjects FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();

-- The analysis referenced by a submission never changes again, not even to OBSOLETE.
CREATE FUNCTION guard_submitted_analysis() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM application_submissions WHERE ocr_analysis_id=OLD.id) THEN
  RAISE EXCEPTION 'Submitted analysis is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER ocr_submitted_immutable BEFORE UPDATE OR DELETE ON ocr_analyses FOR EACH ROW EXECUTE FUNCTION guard_submitted_analysis();

-- A copied subject is the exact value of the confirmed analysis it references.
CREATE FUNCTION check_submission_subject() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM application_submissions s JOIN ocr_extracted_subjects x ON x.ocr_analysis_id=s.ocr_analysis_id
  JOIN subjects sub ON sub.id=x.subject_id
  WHERE s.id=NEW.submission_id AND x.id=NEW.ocr_extracted_subject_id AND x.subject_id=NEW.subject_id
  AND x.position=NEW.position AND x.grade_value=NEW.grade_value AND x.coefficient_value=NEW.coefficient_value
  AND x.grade_raw=NEW.grade_raw AND x.coefficient_raw=NEW.coefficient_raw AND sub.code=NEW.subject_code AND sub.name=NEW.subject_name
 ) THEN RAISE EXCEPTION 'Submitted subject differs from its analysis' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER submission_subject_matches BEFORE INSERT ON application_submission_subjects FOR EACH ROW EXECUTE FUNCTION check_submission_subject();

CREATE FUNCTION check_submission_document() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM application_submissions s JOIN application_documents d ON d.application_id=s.application_id
  JOIN document_versions v ON v.id=d.current_version_id AND v.application_document_id=d.id
  WHERE s.id=NEW.submission_id AND d.document_type=NEW.document_type AND v.id=NEW.document_version_id
  AND v.replaced_at IS NULL AND v.sha256=NEW.sha256
 ) THEN RAISE EXCEPTION 'Submitted document is not the current version' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER submission_document_matches BEFORE INSERT ON application_submission_documents FOR EACH ROW EXECUTE FUNCTION check_submission_document();

-- Checked at commit: a SUBMITTED application always has its complete, consistent snapshot.
CREATE FUNCTION check_submission_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app uuid; s application_submissions%ROWTYPE; expected integer; copied integer; weighted numeric; coefficients numeric;
BEGIN
 IF TG_TABLE_NAME='applications' THEN app:=NEW.id; ELSE app:=NEW.application_id; END IF;
 IF NOT EXISTS (SELECT 1 FROM applications WHERE id=app AND state='SUBMITTED') THEN
  IF TG_TABLE_NAME='applications' THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'Snapshot without submitted application' USING ERRCODE='23514';
 END IF;
 SELECT * INTO s FROM application_submissions WHERE application_id=app;
 IF NOT FOUND THEN RAISE EXCEPTION 'Submitted application without snapshot' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS (
  SELECT 1 FROM applications a JOIN candidates c ON c.id=a.candidate_id
  JOIN admission_campaigns k ON k.id=a.campaign_id
  JOIN campaign_programs cp ON cp.id=a.campaign_program_id JOIN programs p ON p.id=cp.program_id
  JOIN application_documents d ON d.application_id=a.id AND d.document_type='TRANSCRIPT'
  JOIN document_versions v ON v.id=d.current_version_id
  JOIN rule_set_versions g ON g.id=s.rule_set_version_id AND g.campaign_program_id=cp.id
  WHERE a.id=app AND a.campaign_id=s.campaign_id AND a.campaign_program_id=s.campaign_program_id
  AND p.id=s.program_id AND a.origin_qualification_id=s.origin_qualification_id AND a.graduation_year=s.graduation_year
  AND c.last_name=s.last_name AND c.first_names=s.first_names AND c.email=s.email AND c.phone=s.phone
  AND v.id=s.transcript_version_id AND v.sha256=s.transcript_sha256
  AND a.application_number='IFRI-' || k.numbering_year || '-' || p.code || '-' || right(a.application_number,6)
 ) THEN RAISE EXCEPTION 'Snapshot inconsistent with application' USING ERRCODE='23514'; END IF;
 IF (SELECT count(*) FROM application_submission_documents WHERE submission_id=s.id)<>4 THEN
  RAISE EXCEPTION 'Snapshot documents incomplete' USING ERRCODE='23514';
 END IF;
 IF s.validation_path='CANDIDATE_CONFIRMED_OCR' THEN
  SELECT jsonb_array_length(o.expected_subjects) INTO expected FROM ocr_analyses o
  WHERE o.id=s.ocr_analysis_id AND o.application_id=app AND o.status='SUCCEEDED' AND o.candidate_confirmation='CONFIRMED'
  AND o.document_version_id=s.transcript_version_id AND o.rule_grid_version_id=s.rule_set_version_id
  AND o.program_id=s.program_id AND o.origin_qualification_id=s.origin_qualification_id;
  SELECT count(*),sum(grade_value*coefficient_value),sum(coefficient_value) INTO copied,weighted,coefficients
  FROM application_submission_subjects WHERE submission_id=s.id;
  IF expected IS NULL OR copied<>expected OR weighted<>s.weighted_sum OR coefficients<>s.coefficient_sum THEN
   RAISE EXCEPTION 'Snapshot subjects inconsistent' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER application_submission_complete AFTER INSERT OR UPDATE OF state ON applications DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_submission_complete();
CREATE CONSTRAINT TRIGGER submission_snapshot_complete AFTER INSERT ON application_submissions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_submission_complete();

-- Phase 5 refreshed the analysis of any non-DRAFT application to OBSOLETE (empty context): the
-- confirmed analysis of a submitted application must survive its submission. DRAFT behaviour unchanged.
CREATE OR REPLACE FUNCTION refresh_transcript_analysis(app uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE ctx record; expected jsonb;
BEGIN
 IF NOT EXISTS (SELECT 1 FROM applications WHERE id=app AND state='DRAFT') THEN RETURN; END IF;
 SELECT a.id,a.origin_qualification_id,cp.program_id,v.id AS grid_id,d.current_version_id
 INTO ctx FROM applications a
 JOIN campaign_programs cp ON cp.id=a.campaign_program_id AND cp.enabled
 JOIN programs p ON p.id=cp.program_id AND p.enabled
 JOIN origin_qualifications q ON q.id=a.origin_qualification_id AND q.enabled
 JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.status='PUBLISHED'
 JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id AND e.origin_qualification_id=a.origin_qualification_id AND e.eligible
 JOIN application_documents d ON d.application_id=a.id AND d.document_type='TRANSCRIPT'
 WHERE a.id=app AND a.state='DRAFT';
 UPDATE ocr_analyses SET status='OBSOLETE',updated_at=now()
 WHERE application_id=app AND status<>'OBSOLETE' AND
 (ctx.id IS NULL OR document_version_id<>ctx.current_version_id OR rule_grid_version_id<>ctx.grid_id OR origin_qualification_id<>ctx.origin_qualification_id);
 IF ctx.id IS NULL THEN RETURN; END IF;
 -- A candidate rejection applies to the exact PDF version across every context.
 -- Changing program/qualification must not bypass the requirement for a new PDF version.
 IF EXISTS (SELECT 1 FROM ocr_analyses WHERE application_id=app AND document_version_id=ctx.current_version_id AND candidate_confirmation='REJECTED') THEN RETURN; END IF;
 SELECT jsonb_agg(jsonb_build_object('id',s.id,'code',s.code,'name',s.name,'position',r.position) ORDER BY r.position)
 INTO expected FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id AND s.enabled
 JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id
 WHERE e.rule_set_version_id=ctx.grid_id AND e.origin_qualification_id=ctx.origin_qualification_id AND e.eligible;
 IF expected IS NULL OR EXISTS (
  SELECT 1 FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id
  JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id
  WHERE e.rule_set_version_id=ctx.grid_id AND e.origin_qualification_id=ctx.origin_qualification_id AND NOT s.enabled
 ) THEN
  UPDATE ocr_analyses SET status='OBSOLETE',updated_at=now() WHERE application_id=app AND status<>'OBSOLETE';
  RETURN;
 END IF;
 INSERT INTO ocr_analyses(application_id,document_version_id,rule_grid_version_id,program_id,origin_qualification_id,expected_subjects)
 VALUES(app,ctx.current_version_id,ctx.grid_id,ctx.program_id,ctx.origin_qualification_id,expected)
 ON CONFLICT DO NOTHING;
 -- A context revisited must never reactivate an obsolete or rejected result automatically.
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Rolling back would silently destroy submissions, numbers and audit history: refuse instead.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM application_submissions) OR EXISTS (SELECT 1 FROM applications WHERE state<>'DRAFT')
  OR EXISTS (SELECT 1 FROM audit_events) OR EXISTS (SELECT 1 FROM application_number_counters) THEN
  RAISE EXCEPTION 'Submissions exist: migration 00006 rollback refused' USING ERRCODE='55000';
 END IF;
END;
$$;
CREATE OR REPLACE FUNCTION refresh_transcript_analysis(app uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE ctx record; expected jsonb;
BEGIN
 SELECT a.id,a.origin_qualification_id,cp.program_id,v.id AS grid_id,d.current_version_id
 INTO ctx FROM applications a
 JOIN campaign_programs cp ON cp.id=a.campaign_program_id AND cp.enabled
 JOIN programs p ON p.id=cp.program_id AND p.enabled
 JOIN origin_qualifications q ON q.id=a.origin_qualification_id AND q.enabled
 JOIN rule_set_versions v ON v.campaign_program_id=cp.id AND v.status='PUBLISHED'
 JOIN program_qualification_eligibilities e ON e.rule_set_version_id=v.id AND e.origin_qualification_id=a.origin_qualification_id AND e.eligible
 JOIN application_documents d ON d.application_id=a.id AND d.document_type='TRANSCRIPT'
 WHERE a.id=app AND a.state='DRAFT';
 UPDATE ocr_analyses SET status='OBSOLETE',updated_at=now()
 WHERE application_id=app AND status<>'OBSOLETE' AND
 (ctx.id IS NULL OR document_version_id<>ctx.current_version_id OR rule_grid_version_id<>ctx.grid_id OR origin_qualification_id<>ctx.origin_qualification_id);
 IF ctx.id IS NULL THEN RETURN; END IF;
 -- A candidate rejection applies to the exact PDF version across every context.
 -- Changing program/qualification must not bypass the requirement for a new PDF version.
 IF EXISTS (SELECT 1 FROM ocr_analyses WHERE application_id=app AND document_version_id=ctx.current_version_id AND candidate_confirmation='REJECTED') THEN RETURN; END IF;
 SELECT jsonb_agg(jsonb_build_object('id',s.id,'code',s.code,'name',s.name,'position',r.position) ORDER BY r.position)
 INTO expected FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id AND s.enabled
 JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id
 WHERE e.rule_set_version_id=ctx.grid_id AND e.origin_qualification_id=ctx.origin_qualification_id AND e.eligible;
 IF expected IS NULL OR EXISTS (
  SELECT 1 FROM expected_subject_rules r JOIN subjects s ON s.id=r.subject_id
  JOIN program_qualification_eligibilities e ON e.id=r.eligibility_id
  WHERE e.rule_set_version_id=ctx.grid_id AND e.origin_qualification_id=ctx.origin_qualification_id AND NOT s.enabled
 ) THEN
  UPDATE ocr_analyses SET status='OBSOLETE',updated_at=now() WHERE application_id=app AND status<>'OBSOLETE';
  RETURN;
 END IF;
 INSERT INTO ocr_analyses(application_id,document_version_id,rule_grid_version_id,program_id,origin_qualification_id,expected_subjects)
 VALUES(app,ctx.current_version_id,ctx.grid_id,ctx.program_id,ctx.origin_qualification_id,expected)
 ON CONFLICT DO NOTHING;
 -- A context revisited must never reactivate an obsolete or rejected result automatically.
END;
$$;
-- +goose StatementEnd
DROP TRIGGER submission_snapshot_complete ON application_submissions;
DROP TRIGGER application_submission_complete ON applications;
DROP TRIGGER ocr_submitted_immutable ON ocr_analyses;
DROP TRIGGER ocr_subjects_no_truncate ON ocr_extracted_subjects;
DROP TRIGGER ocr_subjects_insert_closed ON ocr_extracted_subjects;
DROP TRIGGER document_versions_locked ON document_versions;
DROP TRIGGER documents_locked ON application_documents;
DROP TRIGGER candidate_locked ON candidates;
DROP TRIGGER application_locked ON applications;
DROP TABLE audit_events;
DROP TABLE application_submission_flags;
DROP TABLE application_submission_documents;
DROP TABLE application_submission_subjects;
DROP TABLE application_submissions;
DROP TABLE application_number_counters;
DROP FUNCTION check_submission_complete();
DROP FUNCTION check_submission_document();
DROP FUNCTION check_submission_subject();
DROP FUNCTION guard_submitted_analysis();
DROP FUNCTION guard_ocr_subject_insert();
DROP FUNCTION guard_locked_documents();
DROP FUNCTION guard_locked_candidate();
DROP FUNCTION guard_locked_application();
DROP FUNCTION guard_number_counter();
DROP FUNCTION guard_append_only();
ALTER TABLE applications DROP CONSTRAINT application_submitted_numbered, DROP CONSTRAINT application_draft_unnumbered,
 DROP COLUMN submitted_at, DROP COLUMN application_number;
ALTER TABLE admission_campaigns DROP CONSTRAINT campaign_numbering_year, DROP COLUMN numbering_year;
