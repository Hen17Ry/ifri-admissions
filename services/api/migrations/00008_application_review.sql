-- +goose Up
-- Phase 8: IFRI roles, review of submitted applications, administrative corrections, effective
-- average, ranking-freeze hook and on-demand OCR proof images. The submission snapshot and
-- applications.state never change: everything here sits beside them.

-- One business role per account, SUPER_ADMIN exclusive. Retired phase 7 roles survive only as
-- revoked history.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM admin_role_assignments WHERE revoked_at IS NULL AND role NOT IN ('SELECTION_MANAGER','AUDITOR')
  GROUP BY admin_user_id HAVING count(DISTINCT role)>1) THEN
  RAISE EXCEPTION 'An account holds several roles: resolve it before migration 00008' USING ERRCODE='55000';
 END IF;
END;
$$;
-- +goose StatementEnd
UPDATE admin_role_assignments SET revoked_at=now() WHERE revoked_at IS NULL AND role IN ('SELECTION_MANAGER','AUDITOR');
ALTER TABLE admin_role_assignments DROP CONSTRAINT admin_role_assignments_role_check,
 DROP CONSTRAINT admin_role_assignments_check,
 ADD CONSTRAINT admin_role_assignments_role_check CHECK (role IN ('SUPER_ADMIN','REVIEWER','REVIEW_LEAD','DEPUTY_DIRECTOR','DIRECTOR') OR (role IN ('SELECTION_MANAGER','AUDITOR') AND revoked_at IS NOT NULL)),
 ADD CONSTRAINT admin_role_assignments_global_check CHECK (role NOT IN ('SUPER_ADMIN','AUDITOR','DEPUTY_DIRECTOR','DIRECTOR') OR program_id IS NULL);

-- Provisional rejection reasons (to confirm with IFRI): the single source of codes and labels.
CREATE TABLE review_rejection_reasons (
 code text PRIMARY KEY CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
 label text NOT NULL CHECK (btrim(label)<>'' AND length(label)<=150),
 position integer NOT NULL UNIQUE CHECK (position>0),
 enabled boolean NOT NULL DEFAULT true
);
INSERT INTO review_rejection_reasons(code,label,position) VALUES
 ('DOCUMENT_ILLEGIBLE','Document illisible',1),
 ('DOCUMENT_INVALID','Document invalide ou erroné',2),
 ('IDENTITY_MISMATCH','Identité non concordante',3),
 ('TRANSCRIPT_INCONSISTENT','Relevé de notes incohérent',4),
 ('QUALIFICATION_NOT_ELIGIBLE','Qualification non éligible',5),
 ('DUPLICATE_APPLICATION','Candidature en double',6),
 ('FRAUD_SUSPECTED','Suspicion de fraude',7),
 ('OTHER','Autre motif',8);

-- Phase 9 writes here when a program's ranking is frozen; afterwards no review change applies.
CREATE TABLE ranking_freezes (
 campaign_program_id uuid PRIMARY KEY REFERENCES campaign_programs(id),
 frozen_at timestamptz NOT NULL DEFAULT now(),
 frozen_by uuid REFERENCES admin_users(id)
);

-- Accent-insensitive search key, without any PostgreSQL extension.
-- +goose StatementBegin
CREATE FUNCTION ifri_fold(value text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT btrim(regexp_replace(lower(translate(
  replace(replace(replace(replace(coalesce(value,''),'Œ','OE'),'œ','oe'),'Æ','AE'),'æ','ae'),
  'ÀÁÂÃÄÅàáâãäåÇçÈÉÊËèéêëÌÍÎÏìíîïÑñÒÓÔÕÖØòóôõöøÙÚÛÜùúûüÝýÿ',
  'AAAAAAaaaaaaCcEEEEeeeeIIIIiiiiNnOOOOOOooooooUUUUuuuuYyy')),'[[:space:]]+',' ','g'))
$$;
-- +goose StatementEnd
-- A stored generated column rewrites the table without firing the append-only row triggers.
ALTER TABLE application_submissions ADD COLUMN name_key text GENERATED ALWAYS AS (ifri_fold(last_name || ' ' || first_names)) STORED;

-- Current review state; one row per submitted application, history in the events table.
CREATE TABLE application_reviews (
 application_id uuid PRIMARY KEY REFERENCES application_submissions(application_id),
 campaign_program_id uuid NOT NULL REFERENCES campaign_programs(id),
 status text NOT NULL CHECK (status IN ('TO_REVIEW','IN_REVIEW','VALIDATED','REJECTED')),
 assignee_id uuid REFERENCES admin_users(id),
 rejection_code text REFERENCES review_rejection_reasons(code),
 rejection_comment text CHECK (rejection_comment IS NULL OR (btrim(rejection_comment)<>'' AND length(rejection_comment)<=2000)),
 version integer NOT NULL CHECK (version>0),
 -- Effective average: the snapshot with approved corrections applied, kept exact (fraction)
 -- plus the 30-decimal truncation; phase 9 ranks on it.
 effective_weighted_sum numeric CHECK (effective_weighted_sum>=0),
 effective_coefficient_sum numeric CHECK (effective_coefficient_sum>0),
 effective_average numeric(32,30) CHECK (effective_average BETWEEN 0 AND 20),
 effective_average_is_exact boolean,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((status='TO_REVIEW')=(assignee_id IS NULL)),
 CHECK ((status='REJECTED')=(rejection_code IS NOT NULL) AND (rejection_code IS NULL)=(rejection_comment IS NULL)),
 CHECK ((effective_average IS NULL)=(effective_weighted_sum IS NULL) AND (effective_average IS NULL)=(effective_coefficient_sum IS NULL) AND (effective_average IS NULL)=(effective_average_is_exact IS NULL)),
 CHECK (effective_average IS NULL OR (
  effective_average*effective_coefficient_sum<=effective_weighted_sum
  AND effective_weighted_sum<(effective_average+0.000000000000000000000000000001)*effective_coefficient_sum
  AND effective_average_is_exact=(effective_average*effective_coefficient_sum=effective_weighted_sum)
 ))
);
CREATE INDEX reviews_status ON application_reviews(status);
CREATE INDEX reviews_assignee ON application_reviews(assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX reviews_effective_average ON application_reviews(effective_average);
CREATE INDEX reviews_program_status ON application_reviews(campaign_program_id,status);

-- Append-only history: exactly one event per review version.
CREATE TABLE application_review_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES application_reviews(application_id),
 version integer NOT NULL CHECK (version>0),
 event_type text NOT NULL CHECK (event_type IN ('CREATED','CLAIMED','VALIDATED','REJECTED','REASSIGNED','REVERTED','CORRECTION_PROPOSED','CORRECTION_APPROVED','CORRECTION_REFUSED')),
 actor_type text NOT NULL CHECK (actor_type IN ('ADMIN','SYSTEM')),
 actor_id uuid REFERENCES admin_users(id),
 from_status text,
 to_status text NOT NULL,
 from_assignee uuid REFERENCES admin_users(id),
 to_assignee uuid REFERENCES admin_users(id),
 reason_code text REFERENCES review_rejection_reasons(code),
 comment text CHECK (comment IS NULL OR length(comment)<=2000),
 correction_id uuid,
 occurred_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (application_id,version),
 CHECK ((actor_type='SYSTEM')=(actor_id IS NULL)),
 CHECK (event_type IN ('CREATED','CLAIMED','VALIDATED','CORRECTION_APPROVED') OR (comment IS NOT NULL AND btrim(comment)<>''))
);

CREATE TABLE application_corrections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES application_reviews(application_id),
 submission_id uuid NOT NULL,
 subject_id uuid NOT NULL,
 field text NOT NULL CHECK (field IN ('GRADE','COEFFICIENT')),
 original_value numeric NOT NULL,
 current_value numeric NOT NULL,
 proposed_value numeric NOT NULL CHECK (proposed_value NOT IN ('NaN'::numeric,'Infinity'::numeric,'-Infinity'::numeric)),
 reason text NOT NULL CHECK (btrim(reason)<>'' AND length(reason)<=2000),
 proposed_by uuid NOT NULL REFERENCES admin_users(id),
 proposed_at timestamptz NOT NULL DEFAULT now(),
 status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVED','REFUSED')),
 decided_by uuid REFERENCES admin_users(id),
 decided_at timestamptz,
 decision_reason text CHECK (decision_reason IS NULL OR length(decision_reason)<=2000),
 FOREIGN KEY (submission_id,subject_id) REFERENCES application_submission_subjects(submission_id,subject_id),
 CHECK (field<>'GRADE' OR proposed_value BETWEEN 0 AND 20),
 CHECK (field<>'COEFFICIENT' OR proposed_value>0),
 CHECK (proposed_value<>current_value),
 CHECK ((status='PENDING')=(decided_by IS NULL) AND (decided_by IS NULL)=(decided_at IS NULL)),
 -- Four eyes, whatever the roles: nobody decides on their own proposal.
 CHECK (decided_by IS DISTINCT FROM proposed_by),
 CHECK (status<>'REFUSED' OR (decision_reason IS NOT NULL AND btrim(decision_reason)<>''))
);
CREATE UNIQUE INDEX corrections_one_pending ON application_corrections(application_id,subject_id,field) WHERE status='PENDING';
CREATE INDEX corrections_pending ON application_corrections(proposed_at) WHERE status='PENDING';
CREATE INDEX corrections_application ON application_corrections(application_id);

-- Proof images: generated on demand by the worker from the exact analysed document version.
CREATE TABLE ocr_proof_images (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 ocr_extracted_subject_id uuid NOT NULL UNIQUE REFERENCES ocr_extracted_subjects(id),
 application_id uuid NOT NULL REFERENCES applications(id),
 status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','READY','FAILED')),
 storage_key text UNIQUE CHECK (storage_key ~ '^applications/[a-f0-9-]{36}/proofs/[a-f0-9-]{36}\.png$'),
 sha256 text CHECK (sha256 ~ '^[a-f0-9]{64}$'),
 size_bytes integer CHECK (size_bytes>0),
 width integer CHECK (width>0),
 height integer CHECK (height>0),
 failure_code text CHECK (failure_code IN ('DOCUMENT_UNAVAILABLE','PAGE_MISSING','RENDER_FAILED','IMAGE_REJECTED')),
 attempts integer NOT NULL DEFAULT 1 CHECK (attempts BETWEEN 1 AND 10),
 requested_at timestamptz NOT NULL DEFAULT now(),
 dispatched_at timestamptz,
 completed_at timestamptz,
 CHECK ((status='READY')=(storage_key IS NOT NULL) AND (storage_key IS NULL)=(sha256 IS NULL)),
 CHECK ((status='FAILED')=(failure_code IS NOT NULL))
);
CREATE INDEX proof_images_pending ON ocr_proof_images(requested_at) WHERE status='PENDING';

CREATE INDEX applications_submitted_at ON applications(submitted_at) WHERE state='SUBMITTED';
CREATE INDEX submissions_email ON application_submissions(email);
CREATE INDEX submissions_qualification ON application_submissions(origin_qualification_id);

-- +goose StatementBegin
CREATE FUNCTION guard_single_role() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Serialize grants of one account, then refuse a second distinct role.
 PERFORM 1 FROM admin_users WHERE id=NEW.admin_user_id FOR UPDATE;
 IF NEW.revoked_at IS NULL AND EXISTS (SELECT 1 FROM admin_role_assignments WHERE admin_user_id=NEW.admin_user_id
  AND revoked_at IS NULL AND role<>NEW.role AND id<>NEW.id) THEN
  RAISE EXCEPTION 'An account holds a single role' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER admin_roles_single BEFORE INSERT ON admin_role_assignments FOR EACH ROW EXECUTE FUNCTION guard_single_role();

CREATE TRIGGER rejection_reasons_no_delete BEFORE DELETE ON review_rejection_reasons FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER ranking_freezes_immutable BEFORE UPDATE OR DELETE ON ranking_freezes FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER review_events_immutable BEFORE UPDATE OR DELETE ON application_review_events FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER reviews_no_delete BEFORE DELETE ON application_reviews FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER corrections_no_delete BEFORE DELETE ON application_corrections FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER proof_images_no_delete BEFORE DELETE ON ocr_proof_images FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER review_events_no_truncate BEFORE TRUNCATE ON application_review_events FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER reviews_no_truncate BEFORE TRUNCATE ON application_reviews FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER corrections_no_truncate BEFORE TRUNCATE ON application_corrections FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();

CREATE FUNCTION ranking_frozen(program uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM ranking_freezes WHERE campaign_program_id=program)
$$;

-- Every submission gets its review, initialised with the snapshot average.
CREATE FUNCTION create_application_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO application_reviews(application_id,campaign_program_id,status,version,effective_weighted_sum,effective_coefficient_sum,effective_average,effective_average_is_exact,updated_at)
 VALUES(NEW.application_id,NEW.campaign_program_id,'TO_REVIEW',1,NEW.weighted_sum,NEW.coefficient_sum,NEW.average,NEW.average_is_exact,NEW.created_at);
 INSERT INTO application_review_events(application_id,version,event_type,actor_type,to_status,occurred_at)
 VALUES(NEW.application_id,1,'CREATED','SYSTEM','TO_REVIEW',NEW.created_at);
 RETURN NULL;
END;
$$;
CREATE TRIGGER submission_review_created AFTER INSERT ON application_submissions FOR EACH ROW EXECUTE FUNCTION create_application_review();

-- A review moves one version at a time, never after the ranking freeze, never to VALIDATED with
-- a pending correction.
CREATE FUNCTION guard_review_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.application_id<>OLD.application_id OR NEW.campaign_program_id<>OLD.campaign_program_id OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'Review identity is immutable and versions move by one' USING ERRCODE='23514';
 END IF;
 IF ranking_frozen(OLD.campaign_program_id) THEN
  RAISE EXCEPTION 'Review frozen by the ranking' USING ERRCODE='23514';
 END IF;
 IF NEW.status='VALIDATED' AND EXISTS (SELECT 1 FROM application_corrections WHERE application_id=NEW.application_id AND status='PENDING') THEN
  RAISE EXCEPTION 'A pending correction blocks validation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER review_update_guard BEFORE UPDATE ON application_reviews FOR EACH ROW EXECUTE FUNCTION guard_review_update();

-- Who may write which event: only the assignee decides; a claim assigns the claimer.
CREATE FUNCTION guard_review_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.event_type IN ('VALIDATED','REJECTED','CORRECTION_PROPOSED') AND NEW.actor_id IS DISTINCT FROM NEW.from_assignee)
  OR (NEW.event_type='CLAIMED' AND (NEW.actor_id IS DISTINCT FROM NEW.to_assignee OR NEW.from_status<>'TO_REVIEW'))
  OR (NEW.event_type='REJECTED' AND NEW.reason_code IS NULL) THEN
  RAISE EXCEPTION 'Review event not allowed for this actor' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER review_event_guard BEFORE INSERT ON application_review_events FOR EACH ROW EXECUTE FUNCTION guard_review_event();

-- At commit: the current state is exactly the state described by the event of its version.
CREATE FUNCTION check_review_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM application_review_events e JOIN application_reviews r ON r.application_id=e.application_id AND r.version=e.version
  WHERE e.application_id=NEW.application_id AND e.to_status=r.status AND e.to_assignee IS NOT DISTINCT FROM r.assignee_id) THEN
  RAISE EXCEPTION 'Review state without its history event' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER review_has_event AFTER INSERT OR UPDATE ON application_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_review_event();

-- At commit: the effective sums are the snapshot with the latest approved correction of each
-- field applied.
CREATE FUNCTION check_effective_average() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r application_reviews%ROWTYPE; weighted numeric; coefficients numeric;
BEGIN
 SELECT * INTO r FROM application_reviews WHERE application_id=NEW.application_id;
 IF NOT FOUND OR r.effective_average IS NULL THEN RETURN NULL; END IF;
 SELECT sum(g*c),sum(c) INTO weighted,coefficients FROM (
  SELECT coalesce((SELECT k.proposed_value FROM application_corrections k WHERE k.application_id=r.application_id AND k.subject_id=x.subject_id
    AND k.field='GRADE' AND k.status='APPROVED' ORDER BY k.decided_at DESC,k.id DESC LIMIT 1),x.grade_value) AS g,
   coalesce((SELECT k.proposed_value FROM application_corrections k WHERE k.application_id=r.application_id AND k.subject_id=x.subject_id
    AND k.field='COEFFICIENT' AND k.status='APPROVED' ORDER BY k.decided_at DESC,k.id DESC LIMIT 1),x.coefficient_value) AS c
  FROM application_submission_subjects x JOIN application_submissions s ON s.id=x.submission_id WHERE s.application_id=r.application_id) t;
 IF weighted IS DISTINCT FROM r.effective_weighted_sum OR coefficients IS DISTINCT FROM r.effective_coefficient_sum THEN
  RAISE EXCEPTION 'Effective average inconsistent with snapshot and approved corrections' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER review_effective_average AFTER INSERT OR UPDATE ON application_reviews DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_effective_average();
CREATE CONSTRAINT TRIGGER correction_effective_average AFTER INSERT OR UPDATE ON application_corrections DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_effective_average();

-- A correction is proposed by the assignee of a dossier in review, decided once, then immutable.
CREATE FUNCTION guard_correction() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE program uuid;
BEGIN
 SELECT campaign_program_id INTO program FROM application_reviews WHERE application_id=NEW.application_id;
 IF ranking_frozen(program) THEN
  RAISE EXCEPTION 'Review frozen by the ranking' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'PENDING' OR NOT EXISTS (SELECT 1 FROM application_reviews WHERE application_id=NEW.application_id AND status='IN_REVIEW' AND assignee_id=NEW.proposed_by)
   OR NOT EXISTS (SELECT 1 FROM application_submissions s JOIN application_submission_subjects x ON x.submission_id=s.id
    WHERE s.application_id=NEW.application_id AND s.id=NEW.submission_id AND x.subject_id=NEW.subject_id
    AND NEW.original_value=CASE NEW.field WHEN 'GRADE' THEN x.grade_value ELSE x.coefficient_value END) THEN
   RAISE EXCEPTION 'Correction must be proposed by the assignee on the snapshot value' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status<>'PENDING' OR NEW.status='PENDING'
  OR ROW(NEW.application_id,NEW.submission_id,NEW.subject_id,NEW.field,NEW.original_value,NEW.current_value,NEW.proposed_value,NEW.reason,NEW.proposed_by,NEW.proposed_at)
   IS DISTINCT FROM ROW(OLD.application_id,OLD.submission_id,OLD.subject_id,OLD.field,OLD.original_value,OLD.current_value,OLD.proposed_value,OLD.reason,OLD.proposed_by,OLD.proposed_at) THEN
  RAISE EXCEPTION 'A correction is decided once and never edited' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER correction_guard BEFORE INSERT OR UPDATE ON application_corrections FOR EACH ROW EXECUTE FUNCTION guard_correction();

-- Existing submissions get their review (phase 6 data).
INSERT INTO application_reviews(application_id,campaign_program_id,status,version,effective_weighted_sum,effective_coefficient_sum,effective_average,effective_average_is_exact,updated_at)
SELECT application_id,campaign_program_id,'TO_REVIEW',1,weighted_sum,coefficient_sum,average,average_is_exact,created_at FROM application_submissions;
INSERT INTO application_review_events(application_id,version,event_type,actor_type,to_status,occurred_at)
SELECT application_id,1,'CREATED','SYSTEM','TO_REVIEW',created_at FROM application_submissions;
-- +goose StatementEnd

-- +goose Down
-- Review decisions, corrections and proofs are history: refuse a rollback that would erase them.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM application_review_events WHERE event_type<>'CREATED') OR EXISTS (SELECT 1 FROM application_corrections)
  OR EXISTS (SELECT 1 FROM ocr_proof_images) OR EXISTS (SELECT 1 FROM ranking_freezes)
  OR EXISTS (SELECT 1 FROM admin_role_assignments WHERE role IN ('REVIEW_LEAD','DEPUTY_DIRECTOR','DIRECTOR')) THEN
  RAISE EXCEPTION 'Review history exists: migration 00008 rollback refused' USING ERRCODE='55000';
 END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER submission_review_created ON application_submissions;
DROP TABLE ocr_proof_images;
DROP TABLE application_corrections;
DROP TABLE application_review_events;
DROP TABLE application_reviews;
DROP FUNCTION check_effective_average();
DROP FUNCTION check_review_event();
DROP FUNCTION guard_review_event();
DROP FUNCTION guard_review_update();
DROP FUNCTION guard_correction();
DROP FUNCTION create_application_review();
DROP FUNCTION ranking_frozen(uuid);
DROP TABLE ranking_freezes;
DROP TABLE review_rejection_reasons;
DROP TRIGGER admin_roles_single ON admin_role_assignments;
DROP FUNCTION guard_single_role();
DROP INDEX submissions_qualification;
DROP INDEX submissions_email;
DROP INDEX applications_submitted_at;
ALTER TABLE application_submissions DROP COLUMN name_key;
DROP FUNCTION ifri_fold(text);
ALTER TABLE admin_role_assignments DROP CONSTRAINT admin_role_assignments_global_check,
 DROP CONSTRAINT admin_role_assignments_role_check,
 ADD CONSTRAINT admin_role_assignments_role_check CHECK (role IN ('SUPER_ADMIN','REVIEWER','SELECTION_MANAGER','AUDITOR')),
 ADD CONSTRAINT admin_role_assignments_check CHECK (role NOT IN ('SUPER_ADMIN','AUDITOR') OR program_id IS NULL);
