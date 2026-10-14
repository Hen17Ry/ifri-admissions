-- +goose Up
CREATE TABLE ocr_analyses (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES applications(id),
 document_version_id uuid NOT NULL REFERENCES document_versions(id),
 rule_grid_version_id uuid NOT NULL REFERENCES rule_set_versions(id),
 program_id uuid NOT NULL REFERENCES programs(id),
 origin_qualification_id uuid NOT NULL REFERENCES origin_qualifications(id),
 contract_version integer NOT NULL DEFAULT 1 CHECK(contract_version=1),
 expected_subjects jsonb NOT NULL CHECK (jsonb_typeof(expected_subjects)='array' AND jsonb_array_length(expected_subjects)>0),
 status text NOT NULL DEFAULT 'QUEUED' CHECK(status IN ('QUEUED','PROCESSING','SUCCEEDED','FAILED','LOW_CONFIDENCE','OBSOLETE')),
 candidate_confirmation text NOT NULL DEFAULT 'PENDING' CHECK(candidate_confirmation IN ('PENDING','CONFIRMED','REJECTED')),
 confirmed_at timestamptz,
 engine text,
 engine_version text,
 attempt_number integer NOT NULL DEFAULT 0 CHECK(attempt_number>=0),
 failure_code text,
 started_at timestamptz,
 completed_at timestamptz,
 dispatched_at timestamptz,
 result_digest text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(candidate_confirmation<>'CONFIRMED' OR (status IN ('SUCCEEDED','OBSOLETE') AND confirmed_at IS NOT NULL))
);
CREATE UNIQUE INDEX ocr_current_context ON ocr_analyses(application_id,document_version_id,rule_grid_version_id,origin_qualification_id) WHERE status<>'OBSOLETE';
CREATE INDEX ocr_queue_pending ON ocr_analyses(created_at) WHERE status='QUEUED';
CREATE TABLE ocr_analysis_attempts (
 ocr_analysis_id uuid NOT NULL REFERENCES ocr_analyses(id),
 attempt_number integer NOT NULL CHECK(attempt_number>0),
 callback_digest text NOT NULL CHECK(callback_digest ~ '^[a-f0-9]{64}$'),
 reported_status text NOT NULL CHECK(reported_status IN ('SUCCEEDED','FAILED','LOW_CONFIDENCE')),
 failure_code text,
 engine text NOT NULL,
 engine_version text NOT NULL,
 received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(ocr_analysis_id,attempt_number)
);
CREATE TABLE ocr_extracted_subjects (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ocr_analysis_id uuid NOT NULL REFERENCES ocr_analyses(id),
 subject_id uuid NOT NULL REFERENCES subjects(id),
 position integer NOT NULL CHECK(position>0),
 detected_subject_text text NOT NULL CHECK(length(detected_subject_text)<=300),
 grade_raw text NOT NULL CHECK(length(grade_raw)<=32),
 grade_value numeric NOT NULL CHECK(grade_value>=0 AND grade_value<=20),
 coefficient_raw text NOT NULL CHECK(length(coefficient_raw)<=32),
 coefficient_value numeric NOT NULL CHECK(coefficient_value>0 AND coefficient_value NOT IN ('NaN'::numeric,'Infinity'::numeric,'-Infinity'::numeric)),
 page_number integer NOT NULL CHECK(page_number>0),
 subject_confidence numeric NOT NULL CHECK(subject_confidence BETWEEN 0 AND 1),
 grade_confidence numeric NOT NULL CHECK(grade_confidence BETWEEN 0 AND 1),
 coefficient_confidence numeric NOT NULL CHECK(coefficient_confidence BETWEEN 0 AND 1),
 overall_confidence numeric NOT NULL CHECK(overall_confidence BETWEEN 0 AND 1),
 evidence jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(ocr_analysis_id,subject_id),
 UNIQUE(ocr_analysis_id,position)
);
-- Automatic transactional invalidation and durable enqueue, including direct academic updates.
-- +goose StatementBegin
CREATE FUNCTION guard_ocr_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'OCR evidence is immutable' USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER ocr_subjects_immutable BEFORE UPDATE OR DELETE ON ocr_extracted_subjects FOR EACH ROW EXECUTE FUNCTION guard_ocr_evidence();
CREATE TRIGGER ocr_attempts_immutable BEFORE UPDATE OR DELETE ON ocr_analysis_attempts FOR EACH ROW EXECUTE FUNCTION guard_ocr_evidence();
CREATE FUNCTION guard_ocr_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.application_id,NEW.document_version_id,NEW.rule_grid_version_id,NEW.program_id,NEW.origin_qualification_id,NEW.expected_subjects,NEW.contract_version)
 IS DISTINCT FROM ROW(OLD.application_id,OLD.document_version_id,OLD.rule_grid_version_id,OLD.program_id,OLD.origin_qualification_id,OLD.expected_subjects,OLD.contract_version)
 THEN RAISE EXCEPTION 'Analysis snapshot is immutable' USING ERRCODE='23514'; END IF;
 IF OLD.candidate_confirmation<>'PENDING' AND (NEW.candidate_confirmation<>OLD.candidate_confirmation OR NEW.confirmed_at IS DISTINCT FROM OLD.confirmed_at)
 THEN RAISE EXCEPTION 'Candidate decision is immutable' USING ERRCODE='23514'; END IF;
 IF OLD.result_digest IS NOT NULL AND (
  ROW(NEW.result_digest,NEW.engine,NEW.engine_version,NEW.failure_code,NEW.completed_at) IS DISTINCT FROM ROW(OLD.result_digest,OLD.engine,OLD.engine_version,OLD.failure_code,OLD.completed_at)
  OR NEW.status NOT IN (OLD.status,'OBSOLETE')
 ) THEN RAISE EXCEPTION 'Analysis result is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER ocr_snapshot_immutable BEFORE UPDATE ON ocr_analyses FOR EACH ROW EXECUTE FUNCTION guard_ocr_snapshot();
CREATE FUNCTION check_ocr_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM document_versions dv JOIN application_documents d ON d.id=dv.application_document_id
  JOIN applications a ON a.id=d.application_id
  JOIN rule_set_versions g ON g.id=NEW.rule_grid_version_id
  JOIN campaign_programs cp ON cp.id=g.campaign_program_id
  JOIN program_qualification_eligibilities e ON e.rule_set_version_id=g.id AND e.origin_qualification_id=NEW.origin_qualification_id AND e.eligible
  WHERE dv.id=NEW.document_version_id AND d.application_id=NEW.application_id AND d.document_type='TRANSCRIPT'
  AND cp.campaign_id=a.campaign_id AND cp.program_id=NEW.program_id AND g.status='PUBLISHED'
 ) THEN RAISE EXCEPTION 'Analysis context ownership invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER ocr_context_ownership BEFORE INSERT OR UPDATE OF application_id,document_version_id,rule_grid_version_id,program_id,origin_qualification_id ON ocr_analyses FOR EACH ROW EXECUTE FUNCTION check_ocr_ownership();
CREATE FUNCTION refresh_transcript_analysis(app uuid) RETURNS void LANGUAGE plpgsql AS $$
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
CREATE FUNCTION trigger_transcript_analysis() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='applications' THEN
  PERFORM refresh_transcript_analysis(NEW.id);
 ELSE
  IF NEW.document_type='TRANSCRIPT' THEN PERFORM refresh_transcript_analysis(NEW.application_id); END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER application_analysis_context AFTER UPDATE OF campaign_program_id,origin_qualification_id,state ON applications FOR EACH ROW EXECUTE FUNCTION trigger_transcript_analysis();
CREATE CONSTRAINT TRIGGER transcript_analysis_context AFTER INSERT OR UPDATE ON application_documents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trigger_transcript_analysis();
SELECT refresh_transcript_analysis(id) FROM applications WHERE state='DRAFT';
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER transcript_analysis_context ON application_documents;
DROP TRIGGER application_analysis_context ON applications;
DROP FUNCTION trigger_transcript_analysis();
DROP FUNCTION refresh_transcript_analysis(uuid);
DROP TABLE ocr_extracted_subjects;
DROP TABLE ocr_analysis_attempts;
DROP TABLE ocr_analyses;
DROP FUNCTION check_ocr_ownership();
DROP FUNCTION guard_ocr_snapshot();
DROP FUNCTION guard_ocr_evidence();
