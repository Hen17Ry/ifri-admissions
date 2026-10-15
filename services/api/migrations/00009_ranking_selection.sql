-- +goose Up
-- Phase 9: ranking, freeze, selection (admitted, waiting list, withdrawals) and exports.
-- The provisional ranking is computed by Go on demand; the freeze writes an immutable snapshot
-- that later events (withdrawals, promotions) never modify. Contract: docs/RANKING_SELECTION.md.

-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM ranking_freezes) THEN
  RAISE EXCEPTION 'A ranking is already frozen: migration 00009 refused' USING ERRCODE='55000';
 END IF;
END;
$$;
-- +goose StatementEnd

-- Subjects whose effective grade breaks ties after the average, identified by code only (never by
-- label). The single place to adjust it.
CREATE TABLE ranking_math_subjects (
 code text PRIMARY KEY CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$')
);
INSERT INTO ranking_math_subjects(code) VALUES ('MATH'),('APPLIED_MATH');

-- Seats (the phase 2 quota column) and the optional minimum average, set by the Director until
-- the freeze.
ALTER TABLE campaign_programs
 ADD COLUMN minimum_average numeric(4,2) CHECK (minimum_average BETWEEN 0 AND 20),
 ADD CONSTRAINT campaign_programs_seats_bounds CHECK (available_seats IS NULL OR available_seats BETWEEN 1 AND 10000);

-- The freeze header records what was applied.
ALTER TABLE ranking_freezes
 ADD COLUMN available_seats integer NOT NULL CHECK (available_seats BETWEEN 1 AND 10000),
 ADD COLUMN minimum_average numeric(4,2) CHECK (minimum_average BETWEEN 0 AND 20),
 ADD COLUMN ranked_count integer NOT NULL CHECK (ranked_count>=0),
 ADD COLUMN not_ranked_count integer NOT NULL CHECK (not_ranked_count>=0),
 ADD COLUMN admitted_count integer NOT NULL,
 ADD COLUMN calculation_version smallint NOT NULL CHECK (calculation_version=1),
 ADD CONSTRAINT ranking_freezes_admitted CHECK (admitted_count=least(available_seats,ranked_count)),
 ALTER COLUMN frozen_by SET NOT NULL;

-- Immutable snapshot of a frozen ranking: one row per validated dossier of the program.
CREATE TABLE ranking_entries (
 campaign_program_id uuid NOT NULL REFERENCES ranking_freezes(campaign_program_id),
 application_id uuid NOT NULL REFERENCES application_reviews(application_id),
 position integer NOT NULL CHECK (position>=1),
 rank integer CHECK (rank>=1),
 weighted_sum numeric NOT NULL CHECK (weighted_sum>=0),
 coefficient_sum numeric NOT NULL CHECK (coefficient_sum>0),
 average numeric(32,30) NOT NULL,
 average_is_exact boolean NOT NULL,
 average_display text NOT NULL CHECK (average_display ~ '^[0-9]{1,2}\.[0-9]{2}$'),
 math_subject_code text REFERENCES ranking_math_subjects(code),
 math_grade numeric CHECK (math_grade BETWEEN 0 AND 20),
 -- NOT_NEEDED: unique average; COMPARED: grades compared in the tie group; SKIPPED: a member of
 -- the tie group has no mathematics subject, so nobody's grade was compared.
 math_tiebreak text NOT NULL CHECK (math_tiebreak IN ('NOT_NEEDED','COMPARED','SKIPPED')),
 submitted_at timestamptz NOT NULL,
 application_number text NOT NULL,
 decisive_criterion text CHECK (decisive_criterion IN ('AVERAGE','MATH','SUBMITTED_AT','APPLICATION_NUMBER')),
 initial_decision text NOT NULL CHECK (initial_decision IN ('ADMITTED','WAITLISTED','NOT_RANKED')),
 PRIMARY KEY (campaign_program_id,application_id),
 UNIQUE (campaign_program_id,position),
 UNIQUE (campaign_program_id,rank),
 CHECK ((rank IS NULL)=(initial_decision='NOT_RANKED')),
 CHECK ((math_grade IS NULL)=(math_subject_code IS NULL)),
 CHECK (math_tiebreak<>'COMPARED' OR math_grade IS NOT NULL),
 CHECK ((decisive_criterion IS NULL)=(position=1)),
 CHECK (decisive_criterion IS DISTINCT FROM 'MATH' OR math_tiebreak='COMPARED')
);

-- Withdrawals and promotions after the freeze: append-only; the current decision of a candidate
-- is derived from the snapshot and these events. Version 1 is the freeze itself.
CREATE TABLE selection_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 campaign_program_id uuid NOT NULL REFERENCES ranking_freezes(campaign_program_id),
 list_version integer NOT NULL CHECK (list_version>=2),
 event_type text NOT NULL CHECK (event_type IN ('WITHDRAWN','PROMOTED')),
 application_id uuid NOT NULL,
 reason text CHECK (reason IS NULL OR (btrim(reason)<>'' AND length(reason)<=2000)),
 actor_id uuid NOT NULL REFERENCES admin_users(id),
 occurred_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (campaign_program_id,list_version),
 FOREIGN KEY (campaign_program_id,application_id) REFERENCES ranking_entries(campaign_program_id,application_id),
 CHECK ((event_type='WITHDRAWN')=(reason IS NOT NULL))
);
CREATE INDEX selection_events_application ON selection_events(campaign_program_id,application_id);

-- PDF exports rendered by the worker (CSV is produced by Go on request and only audited).
CREATE TABLE selection_exports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 campaign_program_id uuid NOT NULL REFERENCES ranking_freezes(campaign_program_id),
 list_version integer NOT NULL CHECK (list_version>=1),
 variant text NOT NULL CHECK (variant IN ('PUBLIC','INTERNAL')),
 status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','READY','FAILED')),
 failure_code text CHECK (failure_code IN ('RENDER_FAILED','DOCUMENT_REJECTED')),
 storage_key text,
 sha256 text CHECK (sha256 ~ '^[0-9a-f]{64}$'),
 size_bytes integer CHECK (size_bytes>0),
 requested_by uuid NOT NULL REFERENCES admin_users(id),
 requested_at timestamptz NOT NULL DEFAULT now(),
 dispatched_at timestamptz,
 completed_at timestamptz,
 CHECK ((status='READY')=(storage_key IS NOT NULL AND sha256 IS NOT NULL AND size_bytes IS NOT NULL)),
 CHECK ((status='FAILED')=(failure_code IS NOT NULL)),
 CHECK ((status='PENDING')=(completed_at IS NULL))
);
CREATE INDEX selection_exports_pending ON selection_exports(requested_at) WHERE status='PENDING';

-- +goose StatementBegin
-- Seats and minimum average are final once the program's ranking is frozen.
CREATE FUNCTION guard_ranking_settings() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ranking_frozen(OLD.id) AND (NEW.available_seats IS DISTINCT FROM OLD.available_seats OR NEW.minimum_average IS DISTINCT FROM OLD.minimum_average) THEN
  RAISE EXCEPTION 'Ranking frozen: seats and minimum average can no longer change' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER campaign_program_ranking_settings BEFORE UPDATE ON campaign_programs FOR EACH ROW EXECUTE FUNCTION guard_ranking_settings();

-- +goose StatementBegin
-- Preconditions of a freeze, checked again in the inserting transaction.
CREATE FUNCTION guard_ranking_freeze() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 offer record;
BEGIN
 SELECT cp.available_seats,cp.minimum_average,c.status,c.ends_at INTO offer
 FROM campaign_programs cp JOIN admission_campaigns c ON c.id=cp.campaign_id WHERE cp.id=NEW.campaign_program_id;
 IF NOT (offer.status IN ('CLOSED','ARCHIVED') OR now()>=offer.ends_at) THEN
  RAISE EXCEPTION 'Freeze refused: the application period is not over' USING ERRCODE='55000';
 END IF;
 IF offer.available_seats IS NULL OR NEW.available_seats<>offer.available_seats OR NEW.minimum_average IS DISTINCT FROM offer.minimum_average THEN
  RAISE EXCEPTION 'Freeze refused: seats and minimum average must be the ones of the offer' USING ERRCODE='55000';
 END IF;
 IF EXISTS (SELECT 1 FROM application_reviews WHERE campaign_program_id=NEW.campaign_program_id AND status IN ('TO_REVIEW','IN_REVIEW')) THEN
  RAISE EXCEPTION 'Freeze refused: dossiers are not all validated or rejected' USING ERRCODE='55000';
 END IF;
 IF EXISTS (SELECT 1 FROM application_corrections k JOIN application_reviews r ON r.application_id=k.application_id
  WHERE r.campaign_program_id=NEW.campaign_program_id AND k.status='PENDING') THEN
  RAISE EXCEPTION 'Freeze refused: a correction is pending' USING ERRCODE='55000';
 END IF;
 NEW.frozen_at:=now();
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ranking_freeze_guard BEFORE INSERT ON ranking_freezes FOR EACH ROW EXECUTE FUNCTION guard_ranking_freeze();

-- +goose StatementBegin
-- Entries are written only by the transaction of their freeze and must copy a validated dossier.
CREATE FUNCTION guard_ranking_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM ranking_freezes WHERE campaign_program_id=NEW.campaign_program_id AND frozen_at=now()) THEN
  RAISE EXCEPTION 'Ranking snapshot is immutable: entries are written with their freeze only' USING ERRCODE='55000';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM application_reviews r JOIN applications a ON a.id=r.application_id
  WHERE r.application_id=NEW.application_id AND r.campaign_program_id=NEW.campaign_program_id AND r.status='VALIDATED'
  AND r.effective_weighted_sum=NEW.weighted_sum AND r.effective_coefficient_sum=NEW.coefficient_sum
  AND r.effective_average=NEW.average AND r.effective_average_is_exact=NEW.average_is_exact
  AND a.application_number=NEW.application_number AND a.submitted_at=NEW.submitted_at) THEN
  RAISE EXCEPTION 'Ranking entry does not match a validated dossier of the program' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ranking_entry_guard BEFORE INSERT ON ranking_entries FOR EACH ROW EXECUTE FUNCTION guard_ranking_entry();

-- +goose StatementBegin
-- At commit: the snapshot covers exactly the validated dossiers, ranks and decisions follow the
-- seats and the threshold, and each adjacent pair is ordered by exact average (cross product)
-- with a decisive criterion consistent with its values. Go computes; this re-verifies.
CREATE FUNCTION check_ranking_freeze() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 validated integer;
 total integer;
 ranked integer;
 admitted integer;
BEGIN
 SELECT count(*) INTO validated FROM application_reviews WHERE campaign_program_id=NEW.campaign_program_id AND status='VALIDATED';
 SELECT count(*),count(rank),count(*) FILTER (WHERE initial_decision='ADMITTED') INTO total,ranked,admitted
 FROM ranking_entries WHERE campaign_program_id=NEW.campaign_program_id;
 IF total<>validated OR ranked<>NEW.ranked_count OR total-ranked<>NEW.not_ranked_count OR admitted<>NEW.admitted_count
  OR coalesce((SELECT max(position) FROM ranking_entries WHERE campaign_program_id=NEW.campaign_program_id),0)<>total
  OR coalesce((SELECT max(rank) FROM ranking_entries WHERE campaign_program_id=NEW.campaign_program_id),0)<>ranked THEN
  RAISE EXCEPTION 'Freeze refused: the snapshot does not cover the validated dossiers' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM ranking_entries WHERE campaign_program_id=NEW.campaign_program_id AND (
   (rank IS NOT NULL AND (initial_decision='ADMITTED')<>(rank<=NEW.admitted_count))
   OR (rank IS NULL)<>(NEW.minimum_average IS NOT NULL AND weighted_sum<NEW.minimum_average*coefficient_sum)
   OR (rank IS NOT NULL AND rank<>position))) THEN
  RAISE EXCEPTION 'Freeze refused: decisions do not follow seats and minimum average' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM ranking_entries a JOIN ranking_entries b ON b.campaign_program_id=a.campaign_program_id AND b.position=a.position+1
  WHERE a.campaign_program_id=NEW.campaign_program_id AND (
   a.weighted_sum*b.coefficient_sum<b.weighted_sum*a.coefficient_sum
   OR (b.decisive_criterion='AVERAGE')<>(a.weighted_sum*b.coefficient_sum<>b.weighted_sum*a.coefficient_sum)
   OR (b.decisive_criterion='MATH' AND NOT (a.math_tiebreak='COMPARED' AND a.math_grade>b.math_grade))
   OR (b.decisive_criterion='SUBMITTED_AT' AND NOT (a.submitted_at<b.submitted_at AND (b.math_tiebreak<>'COMPARED' OR a.math_grade=b.math_grade)))
   OR (b.decisive_criterion='APPLICATION_NUMBER' AND NOT (a.application_number<b.application_number AND a.submitted_at=b.submitted_at
    AND (b.math_tiebreak<>'COMPARED' OR a.math_grade=b.math_grade))))) THEN
  RAISE EXCEPTION 'Freeze refused: the order does not follow the ranking rule' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER ranking_freeze_complete AFTER INSERT ON ranking_freezes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_ranking_freeze();

-- +goose StatementBegin
-- Current decision of a frozen candidate: the snapshot, then withdrawals and promotions.
CREATE FUNCTION selection_state(program uuid,application uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT CASE
  WHEN EXISTS (SELECT 1 FROM selection_events s WHERE s.campaign_program_id=program AND s.application_id=application AND s.event_type='WITHDRAWN') THEN 'WITHDRAWN'
  WHEN EXISTS (SELECT 1 FROM selection_events s WHERE s.campaign_program_id=program AND s.application_id=application AND s.event_type='PROMOTED') THEN 'ADMITTED'
  ELSE (SELECT e.initial_decision FROM ranking_entries e WHERE e.campaign_program_id=program AND e.application_id=application) END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- One event at a time per program (freeze row locked), next list version, allowed transitions,
-- promotions only into a free seat and strictly in waiting-list order.
CREATE FUNCTION guard_selection_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 seats integer;
 current_version integer;
 state text;
 admitted integer;
 first_waiting uuid;
BEGIN
 SELECT available_seats INTO seats FROM ranking_freezes WHERE campaign_program_id=NEW.campaign_program_id FOR UPDATE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'Selection refused: the ranking is not frozen' USING ERRCODE='55000';
 END IF;
 SELECT 1+count(*) INTO current_version FROM selection_events WHERE campaign_program_id=NEW.campaign_program_id;
 IF NEW.list_version<>current_version+1 THEN
  RAISE EXCEPTION 'Selection refused: list version % expected', current_version+1 USING ERRCODE='40001';
 END IF;
 state:=selection_state(NEW.campaign_program_id,NEW.application_id);
 IF NEW.event_type='WITHDRAWN' AND state NOT IN ('ADMITTED','WAITLISTED') THEN
  RAISE EXCEPTION 'Selection refused: only an admitted or waiting candidate can withdraw' USING ERRCODE='55000';
 END IF;
 IF NEW.event_type='PROMOTED' THEN
  IF state<>'WAITLISTED' THEN
   RAISE EXCEPTION 'Selection refused: only a waiting candidate can be promoted' USING ERRCODE='55000';
  END IF;
  SELECT count(*) INTO admitted FROM ranking_entries e
  WHERE e.campaign_program_id=NEW.campaign_program_id AND selection_state(e.campaign_program_id,e.application_id)='ADMITTED';
  IF admitted>=seats THEN
   RAISE EXCEPTION 'Selection refused: no free seat' USING ERRCODE='55000';
  END IF;
  SELECT e.application_id INTO first_waiting FROM ranking_entries e
  WHERE e.campaign_program_id=NEW.campaign_program_id AND selection_state(e.campaign_program_id,e.application_id)='WAITLISTED' ORDER BY e.rank LIMIT 1;
  IF first_waiting IS DISTINCT FROM NEW.application_id THEN
   RAISE EXCEPTION 'Selection refused: promotions follow the waiting-list order' USING ERRCODE='55000';
  END IF;
 END IF;
 NEW.occurred_at:=now();
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER selection_event_guard BEFORE INSERT ON selection_events FOR EACH ROW EXECUTE FUNCTION guard_selection_event();

-- +goose StatementBegin
-- An export moves once, from PENDING to READY or FAILED; its identity never changes.
CREATE FUNCTION guard_selection_export() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status<>'PENDING' OR (NEW.id,NEW.campaign_program_id,NEW.list_version,NEW.variant,NEW.requested_by,NEW.requested_at)
  IS DISTINCT FROM (OLD.id,OLD.campaign_program_id,OLD.list_version,OLD.variant,OLD.requested_by,OLD.requested_at) THEN
  RAISE EXCEPTION 'Selection export already completed' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER selection_export_guard BEFORE UPDATE ON selection_exports FOR EACH ROW EXECUTE FUNCTION guard_selection_export();

CREATE TRIGGER ranking_math_subjects_no_delete BEFORE DELETE ON ranking_math_subjects FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER ranking_entries_immutable BEFORE UPDATE OR DELETE ON ranking_entries FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER selection_events_immutable BEFORE UPDATE OR DELETE ON selection_events FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER selection_exports_no_delete BEFORE DELETE ON selection_exports FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER ranking_freezes_no_truncate BEFORE TRUNCATE ON ranking_freezes FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER ranking_entries_no_truncate BEFORE TRUNCATE ON ranking_entries FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER selection_events_no_truncate BEFORE TRUNCATE ON selection_events FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER selection_exports_no_truncate BEFORE TRUNCATE ON selection_exports FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();

-- +goose Down
-- A frozen ranking, its events and exports are history; thresholds are decisions: refuse a
-- rollback that would erase them.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM ranking_freezes) OR EXISTS (SELECT 1 FROM selection_exports)
  OR EXISTS (SELECT 1 FROM campaign_programs WHERE minimum_average IS NOT NULL) THEN
  RAISE EXCEPTION 'Ranking history exists: migration 00009 rollback refused' USING ERRCODE='55000';
 END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE selection_exports;
DROP TABLE selection_events;
DROP TABLE ranking_entries;
DROP FUNCTION guard_selection_export();
DROP FUNCTION guard_selection_event();
DROP FUNCTION selection_state(uuid,uuid);
DROP TRIGGER ranking_freeze_complete ON ranking_freezes;
DROP TRIGGER ranking_freeze_guard ON ranking_freezes;
DROP TRIGGER ranking_freezes_no_truncate ON ranking_freezes;
DROP FUNCTION check_ranking_freeze();
DROP FUNCTION guard_ranking_entry();
DROP FUNCTION guard_ranking_freeze();
DROP TRIGGER campaign_program_ranking_settings ON campaign_programs;
DROP FUNCTION guard_ranking_settings();
ALTER TABLE ranking_freezes ALTER COLUMN frozen_by DROP NOT NULL,
 DROP CONSTRAINT ranking_freezes_admitted,
 DROP COLUMN calculation_version,
 DROP COLUMN admitted_count,
 DROP COLUMN not_ranked_count,
 DROP COLUMN ranked_count,
 DROP COLUMN minimum_average,
 DROP COLUMN available_seats;
ALTER TABLE campaign_programs DROP CONSTRAINT campaign_programs_seats_bounds, DROP COLUMN minimum_average;
DROP TABLE ranking_math_subjects;
