-- +goose Up
CREATE TABLE candidates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 last_name text NOT NULL DEFAULT '' CHECK (length(last_name)<=100),
 first_names text NOT NULL DEFAULT '' CHECK (length(first_names)<=150),
 email text NOT NULL DEFAULT '' CHECK (length(email)<=254 AND email=lower(btrim(email))),
 phone text NOT NULL DEFAULT '' CHECK (phone='' OR phone ~ '^\+[1-9][0-9]{6,14}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE campaign_programs ADD CONSTRAINT campaign_program_id_campaign_unique UNIQUE(id,campaign_id);
CREATE TABLE applications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 candidate_id uuid NOT NULL REFERENCES candidates(id),
 campaign_id uuid NOT NULL REFERENCES admission_campaigns(id),
 campaign_program_id uuid,
 origin_qualification_id uuid REFERENCES origin_qualifications(id),
 graduation_year integer CHECK (graduation_year BETWEEN 1900 AND 9999),
 state text NOT NULL DEFAULT 'DRAFT' CHECK (state IN ('DRAFT','SUBMITTED','CANCELLED')),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(candidate_id,campaign_id),
 FOREIGN KEY (campaign_program_id,campaign_id) REFERENCES campaign_programs(id,campaign_id),
 CHECK (campaign_program_id IS NULL OR origin_qualification_id IS NOT NULL)
);
CREATE INDEX applications_campaign ON applications(campaign_id);
CREATE TABLE application_draft_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 application_id uuid NOT NULL REFERENCES applications(id),
 secret_hash bytea NOT NULL UNIQUE CHECK (octet_length(secret_hash)=32),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (expires_at>created_at)
);
CREATE INDEX draft_sessions_application ON application_draft_sessions(application_id);
-- +goose Down
DROP TABLE application_draft_sessions;
DROP TABLE applications;
ALTER TABLE campaign_programs DROP CONSTRAINT campaign_program_id_campaign_unique;
DROP TABLE candidates;
