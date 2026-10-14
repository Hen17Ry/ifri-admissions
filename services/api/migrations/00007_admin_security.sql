-- +goose Up
-- Phase 7: administrator accounts, MFA, sessions and role assignments. Accounts are never
-- deleted (audit); credentials are stored only as hashes or authenticated ciphertext.
CREATE TABLE admin_users (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 email text NOT NULL UNIQUE CHECK (email=lower(btrim(email)) AND length(email) BETWEEN 3 AND 254 AND position('@' IN email)>1),
 display_name text NOT NULL CHECK (btrim(display_name)<>'' AND length(display_name)<=150),
 password_hash text CHECK (password_hash IS NULL OR password_hash LIKE '$argon2id$%'),
 status text NOT NULL CHECK (status IN ('PENDING_ACTIVATION','ACTIVE','DISABLED')),
 mfa_enabled_at timestamptz,
 password_changed_at timestamptz,
 last_login_at timestamptz,
 created_by uuid REFERENCES admin_users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 disabled_at timestamptz,
 CHECK (status<>'ACTIVE' OR password_hash IS NOT NULL),
 CHECK ((status='DISABLED')=(disabled_at IS NOT NULL))
);

-- TOTP secret sealed with AES-256-GCM under an HKDF subkey; key_version allows a future rotation.
CREATE TABLE admin_totp_secrets (
 admin_user_id uuid PRIMARY KEY REFERENCES admin_users(id),
 key_version smallint NOT NULL CHECK (key_version>0),
 nonce bytea NOT NULL CHECK (octet_length(nonce)=12),
 ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 17 AND 128),
 confirmed_at timestamptz,
 last_used_step bigint CHECK (last_used_step>0),
 created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE admin_recovery_codes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 admin_user_id uuid NOT NULL REFERENCES admin_users(id),
 code_hash bytea NOT NULL UNIQUE CHECK (octet_length(code_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 used_at timestamptz
);
CREATE INDEX admin_recovery_codes_user ON admin_recovery_codes(admin_user_id) WHERE used_at IS NULL;

CREATE TABLE admin_activation_tokens (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 admin_user_id uuid NOT NULL REFERENCES admin_users(id),
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 created_by uuid REFERENCES admin_users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 used_at timestamptz,
 revoked_at timestamptz,
 CHECK (expires_at>created_at),
 CHECK (used_at IS NULL OR revoked_at IS NULL)
);
CREATE INDEX admin_activation_tokens_user ON admin_activation_tokens(admin_user_id);

-- Server-side sessions, completely separate from candidate draft sessions.
CREATE TABLE admin_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 admin_user_id uuid NOT NULL REFERENCES admin_users(id),
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 stage text NOT NULL CHECK (stage IN ('MFA_ENROLLMENT','MFA_VERIFICATION','ACTIVE')),
 created_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 absolute_expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 revoked_reason text CHECK (revoked_reason IN ('LOGOUT','PASSWORD_CHANGED','ACCOUNT_DISABLED','ROLES_CHANGED','MFA_RESET','EXPIRED','SUPERSEDED')),
 CHECK (absolute_expires_at>created_at),
 CHECK ((revoked_at IS NULL)=(revoked_reason IS NULL))
);
CREATE INDEX admin_sessions_user ON admin_sessions(admin_user_id) WHERE revoked_at IS NULL;

-- Roles are code constants mapped to permissions in Go; only assignments live here.
-- program_id NULL = every program; SUPER_ADMIN and AUDITOR are always global.
CREATE TABLE admin_role_assignments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 admin_user_id uuid NOT NULL REFERENCES admin_users(id),
 role text NOT NULL CHECK (role IN ('SUPER_ADMIN','REVIEWER','SELECTION_MANAGER','AUDITOR')),
 program_id uuid REFERENCES programs(id),
 granted_by uuid REFERENCES admin_users(id),
 granted_at timestamptz NOT NULL DEFAULT now(),
 revoked_by uuid REFERENCES admin_users(id),
 revoked_at timestamptz,
 CHECK (role NOT IN ('SUPER_ADMIN','AUDITOR') OR program_id IS NULL),
 CHECK (revoked_at IS NOT NULL OR revoked_by IS NULL)
);
CREATE UNIQUE INDEX admin_role_assignments_active ON admin_role_assignments(admin_user_id,role,coalesce(program_id,'00000000-0000-0000-0000-000000000000'::uuid)) WHERE revoked_at IS NULL;

-- +goose StatementBegin
-- Accounts and role history are kept for the audit trail.
CREATE TRIGGER admin_users_no_delete BEFORE DELETE ON admin_users FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER admin_users_no_truncate BEFORE TRUNCATE ON admin_users FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER admin_roles_no_delete BEFORE DELETE ON admin_role_assignments FOR EACH ROW EXECUTE FUNCTION guard_append_only();
CREATE TRIGGER admin_roles_no_truncate BEFORE TRUNCATE ON admin_role_assignments FOR EACH STATEMENT EXECUTE FUNCTION guard_append_only();
CREATE FUNCTION guard_role_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- A grant is only ever closed once: who, what and since when never change.
 IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL
  OR ROW(NEW.admin_user_id,NEW.role,NEW.program_id,NEW.granted_by,NEW.granted_at) IS DISTINCT FROM ROW(OLD.admin_user_id,OLD.role,OLD.program_id,OLD.granted_by,OLD.granted_at) THEN
  RAISE EXCEPTION 'Role assignments are only revoked, never edited' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER admin_roles_revoke_only BEFORE UPDATE ON admin_role_assignments FOR EACH ROW EXECUTE FUNCTION guard_role_assignment();

-- Once a number is issued, the campaign numbering year is frozen.
CREATE FUNCTION guard_numbering_year() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.numbering_year IS DISTINCT FROM OLD.numbering_year
  AND EXISTS (SELECT 1 FROM applications WHERE campaign_id=OLD.id AND application_number IS NOT NULL) THEN
  RAISE EXCEPTION 'Numbering year is frozen once an application number is issued' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER campaign_numbering_year_frozen BEFORE UPDATE OF numbering_year ON admission_campaigns FOR EACH ROW EXECUTE FUNCTION guard_numbering_year();
-- +goose StatementEnd

-- +goose Down
-- Administrator accounts are never deleted: refuse a rollback that would drop them.
-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM admin_users) THEN
  RAISE EXCEPTION 'Administrator accounts exist: migration 00007 rollback refused' USING ERRCODE='55000';
 END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER campaign_numbering_year_frozen ON admission_campaigns;
DROP FUNCTION guard_numbering_year();
DROP TABLE admin_role_assignments;
DROP FUNCTION guard_role_assignment();
DROP TABLE admin_sessions;
DROP TABLE admin_activation_tokens;
DROP TABLE admin_recovery_codes;
DROP TABLE admin_totp_secrets;
DROP TABLE admin_users;
