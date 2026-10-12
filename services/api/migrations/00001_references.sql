-- +goose Up
CREATE TABLE admission_campaigns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    academic_year text NOT NULL CHECK (btrim(academic_year) <> ''),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
    status text NOT NULL CHECK (status IN ('DRAFT', 'OPEN', 'CLOSED', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE programs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE origin_qualifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    label text NOT NULL CHECK (btrim(label) <> ''),
    family text NOT NULL CHECK (family IN ('BAC', 'DT')),
    enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE subjects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE CHECK (code ~ '^[A-Z][A-Z0-9_]{0,63}$'),
    name text NOT NULL CHECK (btrim(name) <> ''),
    enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE campaign_programs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES admission_campaigns(id),
    program_id uuid NOT NULL REFERENCES programs(id),
    available_seats integer CHECK (available_seats >= 0),
    enabled boolean NOT NULL DEFAULT true,
    UNIQUE (campaign_id, program_id)
);
-- Versioned at the campaign/program offer level; no duplicated campaign/program keys downstream.
CREATE TABLE rule_set_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_program_id uuid NOT NULL REFERENCES campaign_programs(id),
    version integer NOT NULL CHECK (version > 0),
    source text NOT NULL CHECK (btrim(source) <> ''),
    status text NOT NULL CHECK (status IN ('DRAFT', 'PUBLISHED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (campaign_program_id, version)
);
CREATE UNIQUE INDEX rule_set_one_published ON rule_set_versions(campaign_program_id) WHERE status = 'PUBLISHED';
CREATE TABLE program_qualification_eligibilities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_set_version_id uuid NOT NULL REFERENCES rule_set_versions(id),
    origin_qualification_id uuid NOT NULL REFERENCES origin_qualifications(id),
    eligible boolean NOT NULL,
    UNIQUE (rule_set_version_id, origin_qualification_id)
);
CREATE TABLE expected_subject_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    eligibility_id uuid NOT NULL REFERENCES program_qualification_eligibilities(id),
    subject_id uuid NOT NULL REFERENCES subjects(id),
    position integer NOT NULL CHECK (position > 0),
    UNIQUE (eligibility_id, subject_id),
    UNIQUE (eligibility_id, position)
);
-- +goose StatementBegin
CREATE FUNCTION guard_rule_set_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'DRAFT' THEN RAISE EXCEPTION 'Create a draft before publication' USING ERRCODE = '23514'; END IF;
        RETURN NEW;
    END IF;
    IF OLD.status = 'PUBLISHED' THEN
        RAISE EXCEPTION 'Published rule set is immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.status = 'PUBLISHED' AND (
        NOT EXISTS (SELECT 1 FROM program_qualification_eligibilities WHERE rule_set_version_id = OLD.id AND eligible)
        OR EXISTS (
            SELECT 1 FROM program_qualification_eligibilities e
            LEFT JOIN expected_subject_rules s ON s.eligibility_id = e.id
            WHERE e.rule_set_version_id = OLD.id
            GROUP BY e.id, e.eligible
            HAVING (e.eligible AND (count(s.id) = 0 OR min(s.position) <> 1 OR max(s.position) <> count(s.id)))
                OR (NOT e.eligible AND count(s.id) <> 0)
        )
    ) THEN
        RAISE EXCEPTION 'Incomplete rule set' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER rule_set_guard BEFORE INSERT OR UPDATE OR DELETE ON rule_set_versions FOR EACH ROW EXECUTE FUNCTION guard_rule_set_version();
CREATE FUNCTION guard_rule_child() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    old_version uuid;
    new_version uuid;
    locked_status text;
BEGIN
    IF TG_TABLE_NAME = 'program_qualification_eligibilities' THEN
        IF TG_OP <> 'INSERT' THEN old_version := OLD.rule_set_version_id; END IF;
        IF TG_OP <> 'DELETE' THEN new_version := NEW.rule_set_version_id; END IF;
    ELSE
        IF TG_OP <> 'INSERT' THEN SELECT rule_set_version_id INTO old_version FROM program_qualification_eligibilities WHERE id = OLD.eligibility_id; END IF;
        IF TG_OP <> 'DELETE' THEN SELECT rule_set_version_id INTO new_version FROM program_qualification_eligibilities WHERE id = NEW.eligibility_id; END IF;
    END IF;
    -- Lock parent against concurrent publication; also protect moves out of a published version.
    FOR locked_status IN SELECT status FROM rule_set_versions WHERE id IN (old_version, new_version) ORDER BY id FOR UPDATE LOOP
        IF locked_status = 'PUBLISHED' THEN
            RAISE EXCEPTION 'Published rules are immutable' USING ERRCODE = '23514';
        END IF;
    END LOOP;
    IF TG_TABLE_NAME = 'expected_subject_rules' AND TG_OP <> 'DELETE' AND NOT (
        SELECT eligible FROM program_qualification_eligibilities WHERE id = NEW.eligibility_id
    ) THEN
        RAISE EXCEPTION 'Ineligible combination cannot have expected subjects' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER eligibility_guard BEFORE INSERT OR UPDATE OR DELETE ON program_qualification_eligibilities FOR EACH ROW EXECUTE FUNCTION guard_rule_child();
CREATE TRIGGER expected_subject_guard BEFORE INSERT OR UPDATE OR DELETE ON expected_subject_rules FOR EACH ROW EXECUTE FUNCTION guard_rule_child();
-- +goose StatementEnd

-- +goose Down
DROP TABLE expected_subject_rules;
DROP TABLE program_qualification_eligibilities;
DROP TABLE rule_set_versions;
DROP FUNCTION guard_rule_child();
DROP FUNCTION guard_rule_set_version();
DROP TABLE campaign_programs;
DROP TABLE subjects;
DROP TABLE origin_qualifications;
DROP TABLE programs;
DROP TABLE admission_campaigns;
