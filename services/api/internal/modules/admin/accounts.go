package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/platform/passwords"
)

// GrantInput names a role and, for program-scoped roles, an optional program code.
type GrantInput struct {
	Role    Role   `json:"role"`
	Program string `json:"program"`
}

type AccountInput struct {
	Email       string       `json:"email"`
	DisplayName string       `json:"displayName"`
	Grants      []GrantInput `json:"grants"`
}

type Program struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// lockAccounts serializes account administration (last super-administrator guard).
func lockAccounts(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ifri-admin-accounts',0))`)
	return err
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	_, domain, _ := strings.Cut(email, "@")
	return strings.Contains(domain, ".")
}

func validName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 150 && !strings.ContainsFunc(name, unicode.IsControl)
}

type resolvedGrant struct {
	Role      Role
	ProgramID string
	Code      string
}

// resolveGrants validates roles and program scopes; duplicates are merged.
func resolveGrants(ctx context.Context, tx pgx.Tx, input []GrantInput) ([]resolvedGrant, map[string]string) {
	fields := map[string]string{}
	out := []resolvedGrant{}
	if len(input) == 0 || len(input) > 20 {
		fields["grants"] = "Attribuez au moins un rôle."
		return nil, fields
	}
	for _, g := range input {
		g.Program = strings.ToUpper(strings.TrimSpace(g.Program))
		if !KnownRole(g.Role) {
			fields["grants"] = "Rôle inconnu."
			continue
		}
		if GlobalRoles[g.Role] && g.Program != "" {
			fields["grants"] = "Ce rôle s’applique à toutes les filières et ne peut pas être limité."
			continue
		}
		r := resolvedGrant{Role: g.Role, Code: g.Program}
		if g.Program != "" {
			if err := tx.QueryRow(ctx, `SELECT id::text FROM programs WHERE code=$1`, g.Program).Scan(&r.ProgramID); err != nil {
				fields["grants"] = "Filière inconnue."
				continue
			}
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	// One role per account (several rows only for the scopes of that one role).
	for _, r := range out {
		if r.Role != out[0].Role {
			fields["grants"] = "Un compte n’a qu’un seul rôle : créez un second compte pour un autre rôle."
		}
	}
	return out, fields
}

// Coverage reports missing roles nobody can stand in for (no acting arrangement exists).
type Coverage struct {
	DeputyDirector bool `json:"deputyDirector"`
	Director       bool `json:"director"`
}

func (s *Service) Coverage(ctx context.Context) (Coverage, error) {
	var c Coverage
	err := s.Pool.QueryRow(ctx, `SELECT
	 EXISTS(SELECT 1 FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL WHERE u.status='ACTIVE' AND r.role='DEPUTY_DIRECTOR'),
	 EXISTS(SELECT 1 FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL WHERE u.status='ACTIVE' AND r.role='DIRECTOR')`).Scan(&c.DeputyDirector, &c.Director)
	return c, err
}

func (s *Service) activeSuperAdminsExcept(ctx context.Context, tx pgx.Tx, user string) (int, bool, error) {
	var others int
	var target bool
	err := tx.QueryRow(ctx, `SELECT
	 (SELECT count(DISTINCT u.id) FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL AND r.role='SUPER_ADMIN' WHERE u.status='ACTIVE' AND u.id<>$1::uuid),
	 EXISTS(SELECT 1 FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL AND r.role='SUPER_ADMIN' WHERE u.status='ACTIVE' AND u.id=$1::uuid)`, user).Scan(&others, &target)
	return others, target, err
}

func (s *Service) issueActivation(ctx context.Context, tx pgx.Tx, user, actor string) (string, time.Time, error) {
	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE admin_activation_tokens SET revoked_at=$2 WHERE admin_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, user, now); err != nil {
		return "", time.Time{}, err
	}
	token, hash, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(s.ActivationTTL)
	if _, err = tx.Exec(ctx, `INSERT INTO admin_activation_tokens(admin_user_id,token_hash,created_by,created_at,expires_at) VALUES($1::uuid,$2,nullif($3,'')::uuid,$4,$5)`, user, hash, actor, now, expires); err != nil {
		return "", time.Time{}, err
	}
	// The token travels in the URL fragment: never sent to a server nor in a Referer.
	return s.Origin + "/admin/activation#" + token, expires, nil
}

// Activation is returned once to the creator; only the token hash is stored.
type Activation struct {
	Account   Account   `json:"account"`
	URL       string    `json:"activationUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Service) CreateAccount(ctx context.Context, actor Principal, input AccountInput) (Activation, error) {
	email, name := normalizeEmail(input.Email), strings.TrimSpace(input.DisplayName)
	fields := map[string]string{}
	if !validEmail(email) {
		fields["email"] = "Saisissez une adresse email valide."
	}
	if !validName(name) {
		fields["displayName"] = "Saisissez un nom de 1 à 150 caractères."
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Activation{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return Activation{}, err
	}
	grants, grantFields := resolveGrants(ctx, tx, input.Grants)
	for k, v := range grantFields {
		fields[k] = v
	}
	if len(fields) > 0 {
		return Activation{}, &Problem{Status: 422, Code: "VALIDATION_FAILED", Message: "Vérifiez les informations du compte.", Fields: fields}
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO admin_users(email,display_name,status,created_by) VALUES($1,$2,'PENDING_ACTIVATION',$3::uuid) ON CONFLICT (email) DO NOTHING RETURNING id::text`, email, name, actor.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, ErrAccountExists
	}
	if err != nil {
		return Activation{}, err
	}
	for _, g := range grants {
		if _, err = tx.Exec(ctx, `INSERT INTO admin_role_assignments(admin_user_id,role,program_id,granted_by) VALUES($1::uuid,$2,nullif($3,'')::uuid,$4::uuid)`, id, g.Role, g.ProgramID, actor.UserID); err != nil {
			return Activation{}, err
		}
	}
	url, expires, err := s.issueActivation(ctx, tx, id, actor.UserID)
	if err != nil {
		return Activation{}, err
	}
	if err = s.record(ctx, tx, "ADMIN_ACCOUNT_CREATED", adminActor(actor.UserID), id, map[string]any{"grants": grantCodes(grants)}); err != nil {
		return Activation{}, err
	}
	if err = s.record(ctx, tx, "ADMIN_ACTIVATION_LINK_ISSUED", adminActor(actor.UserID), id, map[string]any{"expiresAt": expires}); err != nil {
		return Activation{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return Activation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Activation{}, err
	}
	return Activation{Account: account, URL: url, ExpiresAt: expires}, nil
}

func grantCodes(grants []resolvedGrant) []string {
	out := []string{}
	for _, g := range grants {
		code := string(g.Role)
		if g.Code != "" {
			code += ":" + g.Code
		}
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}

func (s *Service) ReissueActivation(ctx context.Context, actor Principal, id string) (Activation, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Activation{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return Activation{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return Activation{}, err
	}
	if account.Status != "PENDING_ACTIVATION" {
		return Activation{}, ErrAccountState
	}
	url, expires, err := s.issueActivation(ctx, tx, id, actor.UserID)
	if err != nil {
		return Activation{}, err
	}
	if err = s.record(ctx, tx, "ADMIN_ACTIVATION_LINK_ISSUED", adminActor(actor.UserID), id, map[string]any{"expiresAt": expires, "reissued": true}); err != nil {
		return Activation{}, err
	}
	if account, err = s.account(ctx, tx, id); err != nil {
		return Activation{}, err
	}
	return Activation{Account: account, URL: url, ExpiresAt: expires}, tx.Commit(ctx)
}

// SetStatus disables (revoking sessions and pending links) or re-enables an account.
func (s *Service) SetStatus(ctx context.Context, actor audit.Actor, id string, enabled bool) (Account, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return Account{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return Account{}, err
	}
	now := s.now()
	if enabled {
		if account.Status != "DISABLED" {
			return Account{}, ErrAccountState
		}
		if _, err = tx.Exec(ctx, `UPDATE admin_users SET status=CASE WHEN password_hash IS NULL THEN 'PENDING_ACTIVATION' ELSE 'ACTIVE' END,disabled_at=NULL,updated_at=$2 WHERE id=$1::uuid`, id, now); err != nil {
			return Account{}, err
		}
		err = s.record(ctx, tx, "ADMIN_ACCOUNT_ENABLED", actor, id, nil)
	} else {
		if account.Status == "DISABLED" {
			return Account{}, ErrAccountState
		}
		if actor.Type == audit.ActorAdmin && actor.ID == id {
			return Account{}, ErrSelfAction
		}
		others, target, countErr := s.activeSuperAdminsExcept(ctx, tx, id)
		if countErr != nil {
			return Account{}, countErr
		}
		if target && others == 0 {
			return Account{}, ErrLastSuperAdmin
		}
		if _, err = tx.Exec(ctx, `UPDATE admin_users SET status='DISABLED',disabled_at=$2,updated_at=$2 WHERE id=$1::uuid`, id, now); err != nil {
			return Account{}, err
		}
		if err = revokeSessions(ctx, tx, id, "ACCOUNT_DISABLED", now); err != nil {
			return Account{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE admin_activation_tokens SET revoked_at=$2 WHERE admin_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, id, now); err != nil {
			return Account{}, err
		}
		err = s.record(ctx, tx, "ADMIN_ACCOUNT_DISABLED", actor, id, map[string]any{"sessionsRevoked": true})
	}
	if err != nil {
		return Account{}, err
	}
	if account, err = s.account(ctx, tx, id); err != nil {
		return Account{}, err
	}
	return account, tx.Commit(ctx)
}

// SetGrants replaces the roles and scopes of an account; any change revokes its sessions.
func (s *Service) SetGrants(ctx context.Context, actor Principal, id string, input []GrantInput) (Account, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return Account{}, err
	}
	if _, err = s.account(ctx, tx, id); err != nil {
		return Account{}, err
	}
	wanted, fields := resolveGrants(ctx, tx, input)
	if len(fields) > 0 {
		return Account{}, &Problem{Status: 422, Code: "VALIDATION_FAILED", Message: "Vérifiez les rôles attribués.", Fields: fields}
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.role,coalesce(r.program_id::text,''),coalesce(p.code,'') FROM admin_role_assignments r LEFT JOIN programs p ON p.id=r.program_id WHERE r.admin_user_id=$1::uuid AND r.revoked_at IS NULL`, id)
	if err != nil {
		return Account{}, err
	}
	type held struct {
		id string
		resolvedGrant
	}
	current, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (held, error) {
		var h held
		err := row.Scan(&h.id, &h.Role, &h.ProgramID, &h.Code)
		return h, err
	})
	if err != nil {
		return Account{}, err
	}
	removed, added := []resolvedGrant{}, []resolvedGrant{}
	for _, h := range current {
		if !slices.Contains(wanted, h.resolvedGrant) {
			removed = append(removed, h.resolvedGrant)
		}
	}
	for _, w := range wanted {
		if !slices.ContainsFunc(current, func(h held) bool { return h.resolvedGrant == w }) {
			added = append(added, w)
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		account, err := s.account(ctx, tx, id)
		return account, err
	}
	if slices.ContainsFunc(removed, func(g resolvedGrant) bool { return g.Role == RoleSuperAdmin }) {
		if others, target, err := s.activeSuperAdminsExcept(ctx, tx, id); err != nil {
			return Account{}, err
		} else if target && others == 0 {
			return Account{}, ErrLastSuperAdmin
		}
	}
	now := s.now()
	for _, h := range current {
		if slices.Contains(removed, h.resolvedGrant) {
			if _, err = tx.Exec(ctx, `UPDATE admin_role_assignments SET revoked_at=$2,revoked_by=$3::uuid WHERE id=$1::uuid`, h.id, now, actor.UserID); err != nil {
				return Account{}, err
			}
		}
	}
	for _, g := range added {
		if _, err = tx.Exec(ctx, `INSERT INTO admin_role_assignments(admin_user_id,role,program_id,granted_by,granted_at) VALUES($1::uuid,$2,nullif($3,'')::uuid,$4::uuid,$5)`, id, g.Role, g.ProgramID, actor.UserID, now); err != nil {
			return Account{}, err
		}
	}
	if err = revokeSessions(ctx, tx, id, "ROLES_CHANGED", now); err != nil {
		return Account{}, err
	}
	if err = s.record(ctx, tx, "ADMIN_ROLES_CHANGED", adminActor(actor.UserID), id, map[string]any{"added": grantCodes(added), "removed": grantCodes(removed), "sessionsRevoked": true}); err != nil {
		return Account{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return Account{}, err
	}
	return account, tx.Commit(ctx)
}

// ResetMFA removes the TOTP secret and recovery codes: enrolment again at the next login.
func (s *Service) ResetMFA(ctx context.Context, actor audit.Actor, id string, metadata map[string]any) (Account, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return Account{}, err
	}
	if _, err = s.account(ctx, tx, id); err != nil {
		return Account{}, err
	}
	now := s.now()
	for _, statement := range []string{`DELETE FROM admin_totp_secrets WHERE admin_user_id=$1::uuid`, `DELETE FROM admin_recovery_codes WHERE admin_user_id=$1::uuid`} {
		if _, err = tx.Exec(ctx, statement, id); err != nil {
			return Account{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET mfa_enabled_at=NULL,updated_at=$2 WHERE id=$1::uuid`, id, now); err != nil {
		return Account{}, err
	}
	if err = revokeSessions(ctx, tx, id, "MFA_RESET", now); err != nil {
		return Account{}, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["sessionsRevoked"] = true
	if err = s.record(ctx, tx, "ADMIN_MFA_RESET", actor, id, metadata); err != nil {
		return Account{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return Account{}, err
	}
	return account, tx.Commit(ctx)
}

const accountSQL = `SELECT u.id::text,u.email,u.display_name,u.status,u.mfa_enabled_at IS NOT NULL,u.last_login_at,u.created_at,
 (SELECT max(t.expires_at) FROM admin_activation_tokens t WHERE t.admin_user_id=u.id AND t.used_at IS NULL AND t.revoked_at IS NULL)
 FROM admin_users u`

func scanAccount(row pgx.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Email, &a.DisplayName, &a.Status, &a.MFAEnabled, &a.LastLoginAt, &a.CreatedAt, &a.ActivationExpires)
	return a, err
}

func (s *Service) account(ctx context.Context, tx pgx.Tx, id string) (Account, error) {
	if !validUUID(id) {
		return Account{}, ErrNotFound
	}
	a, err := scanAccount(tx.QueryRow(ctx, accountSQL+` WHERE u.id=$1::uuid FOR UPDATE OF u`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Grants, err = loadGrants(ctx, tx, id)
	a.Grants = nonNil(a.Grants)
	return a, err
}

func validUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.Pool.Query(ctx, accountSQL+` ORDER BY u.status='DISABLED',lower(u.display_name),u.email`)
	if err != nil {
		return nil, err
	}
	accounts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Account, error) { return scanAccount(row) })
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].Grants, err = loadGrants(ctx, s.Pool, accounts[i].ID); err != nil {
			return nil, err
		}
		accounts[i].Grants = nonNil(accounts[i].Grants)
	}
	return accounts, nil
}

func (s *Service) Programs(ctx context.Context) ([]Program, error) {
	rows, err := s.Pool.Query(ctx, `SELECT code,name FROM programs ORDER BY code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Program, error) {
		var p Program
		err := row.Scan(&p.Code, &p.Name)
		return p, err
	})
}

// AuditFilter narrows the read-only journal; Cursor continues a previous page.
type AuditFilter struct {
	EventType string
	ActorType string
	TargetID  string
	From, To  *time.Time
	Cursor    string
	Limit     int
}

type AuditEntry struct {
	ID          string          `json:"id"`
	EventType   string          `json:"eventType"`
	ActorType   string          `json:"actorType"`
	ActorID     string          `json:"actorId,omitempty"`
	ActorLabel  string          `json:"actorLabel,omitempty"`
	TargetType  string          `json:"targetType,omitempty"`
	TargetID    string          `json:"targetId,omitempty"`
	TargetLabel string          `json:"targetLabel,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
	OccurredAt  time.Time       `json:"occurredAt"`
}

type AuditPage struct {
	Events     []AuditEntry `json:"events"`
	NextCursor string       `json:"nextCursor,omitempty"`
	EventTypes []string     `json:"eventTypes"`
}

// EventTypes lists the journal's event types for the filter.
var EventTypes = []string{
	"ADMIN_ACCOUNT_ACTIVATED", "ADMIN_ACCOUNT_CREATED", "ADMIN_ACCOUNT_DISABLED", "ADMIN_ACCOUNT_ENABLED",
	"ADMIN_ACTIVATION_FAILED", "ADMIN_ACTIVATION_LINK_ISSUED", audit.AccessEvent, "ADMIN_LOGIN_FAILED", "ADMIN_LOGIN_LOCKED",
	"ADMIN_LOGIN_PASSWORD_ACCEPTED", "ADMIN_LOGIN_SUCCEEDED", "ADMIN_LOGOUT", "ADMIN_MFA_ENROLLED", "ADMIN_MFA_FAILED",
	"ADMIN_MFA_RESET", "ADMIN_PASSWORD_CHANGE_FAILED", "ADMIN_PASSWORD_CHANGED", "ADMIN_PASSWORD_RESET", "ADMIN_RECOVERY_CODE_USED",
	"ADMIN_RECOVERY_CODES_REGENERATED", "ADMIN_ROLES_CHANGED", "ADMIN_SUPER_ADMIN_BOOTSTRAPPED", "APPLICATION_SUBMITTED",
	"CORRECTION_APPROVED", "CORRECTION_PROPOSED", "CORRECTION_REFUSED", "REVIEW_CLAIMED", "REVIEW_REASSIGNED", "REVIEW_REJECTED",
	"REVIEW_REVERTED", "REVIEW_VALIDATED", "SYNTHETIC_DATA_GENERATED",
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(cursor string) (*time.Time, string, bool) {
	if cursor == "" {
		return nil, "", true
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", false
	}
	stamp, id, found := strings.Cut(string(raw), "|")
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if !found || err != nil || !validUUID(id) {
		return nil, "", false
	}
	return &t, id, true
}

// Audit reads the journal, newest first, by keyset pages.
func (s *Service) Audit(ctx context.Context, f AuditFilter) (AuditPage, error) {
	before, beforeID, ok := decodeCursor(f.Cursor)
	if !ok || (f.EventType != "" && !slices.Contains(EventTypes, f.EventType)) || (f.ActorType != "" && f.ActorType != audit.ActorAdmin && f.ActorType != audit.ActorCandidate && f.ActorType != audit.ActorSystem) || (f.TargetID != "" && !validUUID(f.TargetID)) {
		return AuditPage{}, ErrInvalid
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 50
	}
	rows, err := s.Pool.Query(ctx, `SELECT e.id::text,e.event_type,e.actor_type,coalesce(e.actor_id::text,''),coalesce(a.email,''),coalesce(e.target_type,''),coalesce(e.target_id::text,''),coalesce(t.email,''),e.metadata,e.occurred_at
	 FROM audit_events e
	 LEFT JOIN admin_users a ON e.actor_type='ADMIN' AND a.id=e.actor_id
	 LEFT JOIN admin_users t ON e.target_type='ADMIN_USER' AND t.id=e.target_id
	 WHERE ($1='' OR e.event_type=$1) AND ($2='' OR e.actor_type=$2) AND ($3='' OR e.target_id=nullif($3,'')::uuid)
	 AND ($4::timestamptz IS NULL OR e.occurred_at>=$4) AND ($5::timestamptz IS NULL OR e.occurred_at<$5)
	 AND ($6::timestamptz IS NULL OR (e.occurred_at,e.id)<($6,nullif($7,'')::uuid))
	 ORDER BY e.occurred_at DESC,e.id DESC LIMIT $8`, f.EventType, f.ActorType, f.TargetID, f.From, f.To, before, beforeID, f.Limit+1)
	if err != nil {
		return AuditPage{}, err
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := row.Scan(&e.ID, &e.EventType, &e.ActorType, &e.ActorID, &e.ActorLabel, &e.TargetType, &e.TargetID, &e.TargetLabel, &e.Metadata, &e.OccurredAt)
		return e, err
	})
	if err != nil {
		return AuditPage{}, err
	}
	page := AuditPage{Events: events, EventTypes: EventTypes}
	if len(events) > f.Limit {
		page.Events = events[:f.Limit]
		last := page.Events[f.Limit-1]
		page.NextCursor = encodeCursor(last.OccurredAt, last.ID)
	}
	return page, nil
}

// --- Server-side commands (api admin …): only for whoever runs the binary on the server. ---

// BootstrapSuperAdmin creates the first super-administrator; refused when one is active.
func (s *Service) BootstrapSuperAdmin(ctx context.Context, email, name, password string) (string, error) {
	email, name = normalizeEmail(email), strings.TrimSpace(name)
	if !validEmail(email) || !validName(name) {
		return "", errors.New("a valid --email and --name are required")
	}
	password = passwords.Normalize(password)
	if err := passwords.Check(password, email); err != nil {
		return "", passwordProblem("password", err.Error())
	}
	hashed, err := s.Hasher.Hash(ctx, password)
	if err != nil {
		return "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return "", err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users u JOIN admin_role_assignments r ON r.admin_user_id=u.id AND r.revoked_at IS NULL AND r.role='SUPER_ADMIN' WHERE u.status='ACTIVE')`).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("an active super-administrator already exists: create other accounts from /admin")
	}
	var id string
	now := s.now()
	err = tx.QueryRow(ctx, `INSERT INTO admin_users(email,display_name,password_hash,status,password_changed_at) VALUES($1,$2,$3,'ACTIVE',$4) ON CONFLICT (email) DO NOTHING RETURNING id::text`, email, name, hashed, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("this email already belongs to an account: use reactivate or reset-mfa")
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_role_assignments(admin_user_id,role) VALUES($1::uuid,'SUPER_ADMIN')`, id); err != nil {
		return "", err
	}
	if err = s.record(ctx, tx, "ADMIN_SUPER_ADMIN_BOOTSTRAPPED", audit.Actor{Type: audit.ActorSystem}, id, map[string]any{"via": "CLI"}); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func (s *Service) idByEmail(ctx context.Context, email string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id::text FROM admin_users WHERE email=$1`, normalizeEmail(email)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("no administrator account with this email")
	}
	return id, err
}

// ResetMFAByEmail is the server-side recovery for a lost phone and lost recovery codes.
func (s *Service) ResetMFAByEmail(ctx context.Context, email string) error {
	id, err := s.idByEmail(ctx, email)
	if err != nil {
		return err
	}
	_, err = s.ResetMFA(ctx, audit.Actor{Type: audit.ActorSystem}, id, map[string]any{"via": "CLI"})
	return err
}

// ReactivateByEmail re-enables a disabled account from the server (SYSTEM audit).
func (s *Service) ReactivateByEmail(ctx context.Context, email string) error {
	id, err := s.idByEmail(ctx, email)
	if err != nil {
		return err
	}
	_, err = s.SetStatus(ctx, audit.Actor{Type: audit.ActorSystem}, id, true)
	return err
}

// ResetPasswordByEmail is the server-side recovery for a forgotten password (also of the last
// super-administrator): the password is cleared, sessions are revoked and a single-use
// activation link is returned to the operator. The second factor is kept.
func (s *Service) ResetPasswordByEmail(ctx context.Context, email string) (string, time.Time, error) {
	id, err := s.idByEmail(ctx, email)
	if err != nil {
		return "", time.Time{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = lockAccounts(ctx, tx); err != nil {
		return "", time.Time{}, err
	}
	account, err := s.account(ctx, tx, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if account.Status == "DISABLED" {
		return "", time.Time{}, errors.New("account disabled: reactivate it first")
	}
	now := s.now()
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET password_hash=NULL,status='PENDING_ACTIVATION',updated_at=$2 WHERE id=$1::uuid`, id, now); err != nil {
		return "", time.Time{}, err
	}
	if err = revokeSessions(ctx, tx, id, "PASSWORD_CHANGED", now); err != nil {
		return "", time.Time{}, err
	}
	url, expires, err := s.issueActivation(ctx, tx, id, "")
	if err != nil {
		return "", time.Time{}, err
	}
	if err = s.record(ctx, tx, "ADMIN_PASSWORD_RESET", audit.Actor{Type: audit.ActorSystem}, id, map[string]any{"via": "CLI", "expiresAt": expires, "sessionsRevoked": true}); err != nil {
		return "", time.Time{}, err
	}
	return url, expires, tx.Commit(ctx)
}
