-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION guard_rule_child() RETURNS trigger LANGUAGE plpgsql AS $$
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
    -- Separate procedural branches: eligibility rows have no eligibility_id field.
    -- SQL expressions are planned before execution; AND is not a record-shape guard.
    IF TG_TABLE_NAME = 'expected_subject_rules' THEN
        IF TG_OP <> 'DELETE' THEN
            IF NOT (SELECT eligible FROM program_qualification_eligibilities WHERE id = NEW.eligibility_id) THEN
                RAISE EXCEPTION 'Ineligible combination cannot have expected subjects' USING ERRCODE = '23514';
            END IF;
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restore the function from schema version 1; roll forward again before seeding.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION guard_rule_child() RETURNS trigger LANGUAGE plpgsql AS $$
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
-- +goose StatementEnd
