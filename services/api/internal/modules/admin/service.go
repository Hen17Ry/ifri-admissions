package admin

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/platform/passwords"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
	"ifri-admissions/services/api/internal/platform/totp"
)

// Hasher is the argon2id port (counted in tests to prove a refused request costs nothing).
type Hasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(ctx context.Context, encoded, password string) (bool, error)
	VerifyDummy(ctx context.Context, password string) error
	NeedsRehash(encoded string) bool
}

// Rate limit policies. Every key is a pseudonym (HMAC), checked before any argon2 work.
var (
	PolicyLoginIP      = ratelimit.Policy{Name: "login-ip", MaxAttempts: 30, Window: 15 * time.Minute, FreeFailures: 10, BaseDelay: time.Second, MaxDelay: 30 * time.Second, LockAfter: 30, LockFor: 15 * time.Minute}
	PolicyLoginAccount = ratelimit.Policy{Name: "login-account", MaxAttempts: 15, Window: 15 * time.Minute, FreeFailures: 3, BaseDelay: 2 * time.Second, MaxDelay: time.Minute, LockAfter: 10, LockFor: 15 * time.Minute}
	PolicyMFA          = ratelimit.Policy{Name: "mfa-account", MaxAttempts: 10, Window: 15 * time.Minute, FreeFailures: 3, BaseDelay: 2 * time.Second, MaxDelay: time.Minute, LockAfter: 6, LockFor: 15 * time.Minute}
	PolicyActivationIP = ratelimit.Policy{Name: "activation-ip", MaxAttempts: 20, Window: 15 * time.Minute, FreeFailures: 5, BaseDelay: time.Second, MaxDelay: 30 * time.Second, LockAfter: 10, LockFor: 15 * time.Minute}
	PolicyPassword     = ratelimit.Policy{Name: "password-change", MaxAttempts: 10, Window: 15 * time.Minute, FreeFailures: 3, BaseDelay: 2 * time.Second, MaxDelay: time.Minute, LockAfter: 6, LockFor: 15 * time.Minute}
)

const issuer = "IFRI Admissions"

type Service struct {
	Pool          *pgxpool.Pool
	Keys          secrets.Keys
	Hasher        Hasher
	Limiter       ratelimit.Limiter
	Logger        *slog.Logger
	MFARequired   bool
	Idle          time.Duration
	Absolute      time.Duration
	ActivationTTL time.Duration
	Origin        string
	Now           func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Session is an authenticated admin session at one of its stages.
type Session struct {
	ID         string
	Stage      string
	TokenHash  []byte
	MFAEnabled bool
	LastSeen   time.Time
	Absolute   time.Time
	Principal  Principal
}

func (s *Service) View(session *Session) SessionView {
	p := session.Principal
	idle := session.LastSeen.Add(s.Idle)
	if idle.After(session.Absolute) {
		idle = session.Absolute
	}
	return SessionView{Stage: session.Stage, CSRFToken: s.Keys.CSRF(session.TokenHash), Email: p.Email, DisplayName: p.DisplayName,
		Permissions: p.Permissions(), Grants: nonNil(p.Grants), IdleExpires: idle, Expires: session.Absolute, MFARequired: s.MFARequired}
}

func nonNil(g []Grant) []Grant {
	if g == nil {
		return []Grant{}
	}
	return g
}

func (s *Service) record(ctx context.Context, db audit.Execer, event string, actor audit.Actor, targetID string, metadata map[string]any) error {
	target := ""
	if targetID != "" {
		target = "ADMIN_USER"
	}
	return audit.Record(ctx, db, event, actor, target, targetID, metadata)
}

func adminActor(id string) audit.Actor { return audit.Actor{Type: audit.ActorAdmin, ID: id} }

// recordNow writes an event outside any business transaction (failures, logout).
func (s *Service) recordNow(ctx context.Context, event string, actor audit.Actor, targetID string, metadata map[string]any) {
	if err := s.record(ctx, s.Pool, event, actor, targetID, metadata); err != nil {
		s.Logger.Error("admin audit write failed", "event_type", event)
	}
}

// Authenticate resolves a session cookie; idle and absolute expirations revoke it.
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, error) {
	if !validToken(token) {
		return nil, ErrUnauthenticated
	}
	hash := hashToken(token)
	session := &Session{TokenHash: hash}
	var status string
	var mfa *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT s.id::text,s.stage,s.last_seen_at,s.absolute_expires_at,u.id::text,u.email,u.display_name,u.status,u.mfa_enabled_at
	 FROM admin_sessions s JOIN admin_users u ON u.id=s.admin_user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL`, hash).
		Scan(&session.ID, &session.Stage, &session.LastSeen, &session.Absolute, &session.Principal.UserID, &session.Principal.Email, &session.Principal.DisplayName, &status, &mfa)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if status != "ACTIVE" {
		return nil, ErrUnauthenticated
	}
	if !now.Before(session.Absolute) || !now.Before(session.LastSeen.Add(s.Idle)) {
		if _, err = s.Pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at=$2,revoked_reason='EXPIRED' WHERE id=$1::uuid AND revoked_at IS NULL`, session.ID, now); err != nil {
			return nil, err
		}
		return nil, ErrSessionExpired
	}
	session.MFAEnabled = mfa != nil
	if session.Principal.Grants, err = loadGrants(ctx, s.Pool, session.Principal.UserID); err != nil {
		return nil, err
	}
	if now.Sub(session.LastSeen) > 15*time.Second {
		if _, err = s.Pool.Exec(ctx, `UPDATE admin_sessions SET last_seen_at=$2 WHERE id=$1::uuid AND revoked_at IS NULL`, session.ID, now); err != nil {
			return nil, err
		}
		session.LastSeen = now
	}
	return session, nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadGrants(ctx context.Context, db querier, user string) ([]Grant, error) {
	rows, err := db.Query(ctx, `SELECT r.role,coalesce(r.program_id::text,''),coalesce(p.code,''),coalesce(p.name,'')
	 FROM admin_role_assignments r LEFT JOIN programs p ON p.id=r.program_id
	 WHERE r.admin_user_id=$1::uuid AND r.revoked_at IS NULL ORDER BY r.role,p.code`, user)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Grant, error) {
		var g Grant
		err := row.Scan(&g.Role, &g.ProgramID, &g.ProgramCode, &g.ProgramName)
		return g, err
	})
}

// limit checks a rate-limit key; a blocked key is refused before any expensive work.
func (s *Service) limit(ctx context.Context, p ratelimit.Policy, key string) error {
	retry, err := s.Limiter.Attempt(ctx, p, key)
	if err != nil {
		s.Logger.Error("admin rate limiter unavailable", "policy", p.Name)
		return ErrUnavailable
	}
	if retry > 0 {
		return rateLimited(retry)
	}
	return nil
}

func (s *Service) fail(ctx context.Context, p ratelimit.Policy, key string) time.Duration {
	delay, err := s.Limiter.Failure(ctx, p, key)
	if err != nil {
		s.Logger.Error("admin rate limiter unavailable", "policy", p.Name)
	}
	return delay
}

type loginAccount struct {
	id, status, hash string
	mfa              bool
}

// Login checks the IP and identifier limits first, then argon2 (a dummy hash for an unknown,
// pending or disabled account), and opens a session at the second-factor stage.
func (s *Service) Login(ctx context.Context, email, password, ip, previous string) (string, string, error) {
	email = normalizeEmail(email)
	ipKey, idKey := s.Keys.Pseudonym("ip", ip), s.Keys.Pseudonym("email", email)
	meta := map[string]any{"identifierHash": idKey, "ipHash": ipKey}
	if err := s.limit(ctx, PolicyLoginIP, ipKey); err != nil {
		return "", "", err
	}
	if err := s.limit(ctx, PolicyLoginAccount, idKey); err != nil {
		return "", "", err
	}
	var account loginAccount
	var hash *string
	var mfa *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT id::text,status,password_hash,mfa_enabled_at FROM admin_users WHERE email=$1`, email).Scan(&account.id, &account.status, &hash, &mfa)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", err
	}
	ok, reason := false, "UNKNOWN_ACCOUNT"
	if err == nil && account.status == "ACTIVE" && hash != nil {
		account.hash, account.mfa, reason = *hash, mfa != nil, "WRONG_PASSWORD"
		if ok, err = s.Hasher.Verify(ctx, account.hash, password); err != nil {
			return "", "", err
		}
	} else {
		if err == nil {
			reason = "ACCOUNT_" + account.status
		}
		if err = s.Hasher.VerifyDummy(ctx, password); err != nil {
			return "", "", err
		}
	}
	if !ok {
		s.fail(ctx, PolicyLoginIP, ipKey)
		delay := s.fail(ctx, PolicyLoginAccount, idKey)
		meta["reason"] = reason
		target := ""
		if reason != "UNKNOWN_ACCOUNT" {
			target = account.id
		}
		s.recordNow(ctx, "ADMIN_LOGIN_FAILED", audit.Actor{Type: audit.ActorSystem}, target, meta)
		if delay >= PolicyLoginAccount.LockFor {
			s.recordNow(ctx, "ADMIN_LOGIN_LOCKED", audit.Actor{Type: audit.ActorSystem}, target, map[string]any{"identifierHash": idKey, "lockSeconds": int(delay.Seconds())})
		}
		return "", "", ErrInvalidCredentials
	}
	if err = s.Limiter.Reset(ctx, PolicyLoginAccount, idKey); err != nil {
		s.Logger.Error("admin rate limiter unavailable", "policy", PolicyLoginAccount.Name)
	}
	if s.Hasher.NeedsRehash(account.hash) {
		if upgraded, err := s.Hasher.Hash(ctx, password); err == nil {
			_, _ = s.Pool.Exec(ctx, `UPDATE admin_users SET password_hash=$2,updated_at=now() WHERE id=$1::uuid`, account.id, upgraded)
		}
	}
	stage := StageActive
	if account.mfa {
		stage = StageVerification
	} else if s.MFARequired {
		stage = StageEnrollment
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return "", "", err
	}
	now := s.now()
	absolute := now.Add(preAuthLifetime)
	if stage == StageActive {
		absolute = now.Add(s.Absolute)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if validToken(previous) {
		// A new login replaces the session this browser held.
		if _, err = tx.Exec(ctx, `UPDATE admin_sessions SET revoked_at=$2,revoked_reason='SUPERSEDED' WHERE token_hash=$1 AND revoked_at IS NULL`, hashToken(previous), now); err != nil {
			return "", "", err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_sessions(admin_user_id,token_hash,stage,created_at,last_seen_at,absolute_expires_at) VALUES($1::uuid,$2,$3,$4,$4,$5)`, account.id, tokenHash, stage, now, absolute); err != nil {
		return "", "", err
	}
	event := "ADMIN_LOGIN_PASSWORD_ACCEPTED"
	if stage == StageActive {
		event = "ADMIN_LOGIN_SUCCEEDED"
		meta["secondFactor"] = "NOT_REQUIRED"
		if _, err = tx.Exec(ctx, `UPDATE admin_users SET last_login_at=$2 WHERE id=$1::uuid`, account.id, now); err != nil {
			return "", "", err
		}
	}
	delete(meta, "identifierHash")
	if err = s.record(ctx, tx, event, adminActor(account.id), account.id, meta); err != nil {
		return "", "", err
	}
	return token, stage, tx.Commit(ctx)
}

// promote rotates the session token as it becomes ACTIVE after the second factor.
func (s *Service) promote(ctx context.Context, tx pgx.Tx, session *Session, method, ip string) (string, error) {
	token, tokenHash, err := newToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	tag, err := tx.Exec(ctx, `UPDATE admin_sessions SET token_hash=$2,stage='ACTIVE',last_seen_at=$3,absolute_expires_at=$4 WHERE id=$1::uuid AND stage=$5 AND revoked_at IS NULL`,
		session.ID, tokenHash, now, now.Add(s.Absolute), session.Stage)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", ErrUnauthenticated
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET last_login_at=$2 WHERE id=$1::uuid`, session.Principal.UserID, now); err != nil {
		return "", err
	}
	if err = s.record(ctx, tx, "ADMIN_LOGIN_SUCCEEDED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"secondFactor": method, "ipHash": s.Keys.Pseudonym("ip", ip)}); err != nil {
		return "", err
	}
	return token, nil
}

// Enrollment is shown once, during the enrolment stage only.
type Enrollment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	QRSVG  string `json:"qrSvg"`
}

func aad(user string) []byte { return []byte("ifri-admin-totp:" + user) }

// StartEnrollment creates (or replaces) the pending, unconfirmed TOTP secret.
func (s *Service) StartEnrollment(ctx context.Context, session *Session) (Enrollment, error) {
	secret, err := totp.NewSecret()
	if err != nil {
		return Enrollment{}, err
	}
	version, nonce, sealed, err := s.Keys.Seal(secret, aad(session.Principal.UserID))
	if err != nil {
		return Enrollment{}, err
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO admin_totp_secrets(admin_user_id,key_version,nonce,ciphertext) VALUES($1::uuid,$2,$3,$4)
	 ON CONFLICT (admin_user_id) DO UPDATE SET key_version=EXCLUDED.key_version,nonce=EXCLUDED.nonce,ciphertext=EXCLUDED.ciphertext,created_at=now(),last_used_step=NULL
	 WHERE admin_totp_secrets.confirmed_at IS NULL`, session.Principal.UserID, version, nonce, sealed)
	if err != nil {
		return Enrollment{}, err
	}
	if tag.RowsAffected() != 1 {
		return Enrollment{}, ErrAccountState
	}
	uri := totp.URI(issuer, session.Principal.Email, secret)
	svg, err := totp.QRSVG(uri)
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: totp.EncodeSecret(secret), URI: uri, QRSVG: svg}, nil
}

func (s *Service) secret(ctx context.Context, db pgx.Tx, user string, confirmed bool) ([]byte, int64, error) {
	var version int
	var nonce, sealed []byte
	var last *int64
	var confirmedAt *time.Time
	err := db.QueryRow(ctx, `SELECT key_version,nonce,ciphertext,last_used_step,confirmed_at FROM admin_totp_secrets WHERE admin_user_id=$1::uuid FOR UPDATE`, user).Scan(&version, &nonce, &sealed, &last, &confirmedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (confirmedAt != nil) != confirmed) {
		return nil, 0, ErrAccountState
	}
	if err != nil {
		return nil, 0, err
	}
	secret, err := s.Keys.Open(version, nonce, sealed, aad(user))
	if err != nil {
		s.Logger.Error("TOTP secret cannot be opened", "admin_user_id", user, "key_version", version)
		return nil, 0, ErrUnavailable
	}
	var step int64
	if last != nil {
		step = *last
	}
	return secret, step, nil
}

func (s *Service) issueRecoveryCodes(ctx context.Context, tx pgx.Tx, user string) ([]string, error) {
	// Regenerating always invalidates every previous code.
	if _, err := tx.Exec(ctx, `DELETE FROM admin_recovery_codes WHERE admin_user_id=$1::uuid`, user); err != nil {
		return nil, err
	}
	codes := make([]string, 0, recoveryCodes)
	for len(codes) < recoveryCodes {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO admin_recovery_codes(admin_user_id,code_hash) VALUES($1::uuid,$2)`, user, hashRecoveryCode(code)); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// ConfirmEnrollment validates the first code, enables MFA, issues recovery codes and promotes.
func (s *Service) ConfirmEnrollment(ctx context.Context, session *Session, code, ip string) (string, []string, error) {
	key := s.Keys.Pseudonym("admin", session.Principal.UserID)
	if err := s.limit(ctx, PolicyMFA, key); err != nil {
		return "", nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	secret, last, err := s.secret(ctx, tx, session.Principal.UserID, false)
	if err != nil {
		return "", nil, err
	}
	step, ok := totp.Verify(secret, code, s.now(), last)
	if !ok {
		_ = tx.Rollback(ctx)
		s.fail(ctx, PolicyMFA, key)
		s.recordNow(ctx, "ADMIN_MFA_FAILED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"stage": StageEnrollment})
		return "", nil, ErrMFAInvalid
	}
	now := s.now()
	if _, err = tx.Exec(ctx, `UPDATE admin_totp_secrets SET confirmed_at=$2,last_used_step=$3 WHERE admin_user_id=$1::uuid`, session.Principal.UserID, now, step); err != nil {
		return "", nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET mfa_enabled_at=$2,updated_at=$2 WHERE id=$1::uuid`, session.Principal.UserID, now); err != nil {
		return "", nil, err
	}
	codes, err := s.issueRecoveryCodes(ctx, tx, session.Principal.UserID)
	if err != nil {
		return "", nil, err
	}
	if err = s.record(ctx, tx, "ADMIN_MFA_ENROLLED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"method": "TOTP", "recoveryCodes": len(codes)}); err != nil {
		return "", nil, err
	}
	token, err := s.promote(ctx, tx, session, "TOTP_ENROLLMENT", ip)
	if err != nil {
		return "", nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	_ = s.Limiter.Reset(ctx, PolicyMFA, key)
	return token, codes, nil
}

// VerifyMFA accepts a TOTP code (current step ±1, never twice) or an unused recovery code.
func (s *Service) VerifyMFA(ctx context.Context, session *Session, code, ip string) (string, error) {
	key := s.Keys.Pseudonym("admin", session.Principal.UserID)
	if err := s.limit(ctx, PolicyMFA, key); err != nil {
		return "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	method, ok := "TOTP", false
	remaining := -1
	if looksLikeRecoveryCode(code) {
		method = "RECOVERY_CODE"
		tag, err := tx.Exec(ctx, `UPDATE admin_recovery_codes SET used_at=$3 WHERE admin_user_id=$1::uuid AND code_hash=$2 AND used_at IS NULL`, session.Principal.UserID, hashRecoveryCode(code), s.now())
		if err != nil {
			return "", err
		}
		ok = tag.RowsAffected() == 1
		if ok {
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM admin_recovery_codes WHERE admin_user_id=$1::uuid AND used_at IS NULL`, session.Principal.UserID).Scan(&remaining); err != nil {
				return "", err
			}
		}
	} else {
		secret, last, err := s.secret(ctx, tx, session.Principal.UserID, true)
		if err != nil {
			return "", err
		}
		var step int64
		if step, ok = totp.Verify(secret, code, s.now(), last); ok {
			tag, err := tx.Exec(ctx, `UPDATE admin_totp_secrets SET last_used_step=$2 WHERE admin_user_id=$1::uuid AND (last_used_step IS NULL OR last_used_step<$2)`, session.Principal.UserID, step)
			if err != nil {
				return "", err
			}
			ok = tag.RowsAffected() == 1
		}
	}
	if !ok {
		_ = tx.Rollback(ctx)
		s.fail(ctx, PolicyMFA, key)
		s.recordNow(ctx, "ADMIN_MFA_FAILED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"stage": StageVerification, "method": method, "ipHash": s.Keys.Pseudonym("ip", ip)})
		return "", ErrMFAInvalid
	}
	if method == "RECOVERY_CODE" {
		if err = s.record(ctx, tx, "ADMIN_RECOVERY_CODE_USED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"remaining": remaining}); err != nil {
			return "", err
		}
	}
	token, err := s.promote(ctx, tx, session, method, ip)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	_ = s.Limiter.Reset(ctx, PolicyMFA, key)
	return token, nil
}

func (s *Service) Logout(ctx context.Context, session *Session) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `UPDATE admin_sessions SET revoked_at=$2,revoked_reason='LOGOUT' WHERE id=$1::uuid AND revoked_at IS NULL`, session.ID, s.now()); err != nil {
		return err
	}
	if err = s.record(ctx, tx, "ADMIN_LOGOUT", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"stage": session.Stage}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func revokeSessions(ctx context.Context, tx pgx.Tx, user, reason string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE admin_sessions SET revoked_at=$2,revoked_reason=$3 WHERE admin_user_id=$1::uuid AND revoked_at IS NULL`, user, now, reason)
	return err
}

// ChangePassword re-verifies the current password, then revokes every session, this one too.
func (s *Service) ChangePassword(ctx context.Context, session *Session, current, next string) error {
	key := s.Keys.Pseudonym("admin", session.Principal.UserID)
	if err := s.limit(ctx, PolicyPassword, key); err != nil {
		return err
	}
	next = passwords.Normalize(next)
	if err := passwords.Check(next, session.Principal.Email); err != nil {
		return passwordProblem("newPassword", err.Error())
	}
	var hash string
	if err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM admin_users WHERE id=$1::uuid AND status='ACTIVE'`, session.Principal.UserID).Scan(&hash); err != nil {
		return err
	}
	ok, err := s.Hasher.Verify(ctx, hash, current)
	if err != nil {
		return err
	}
	if !ok {
		s.fail(ctx, PolicyPassword, key)
		s.recordNow(ctx, "ADMIN_PASSWORD_CHANGE_FAILED", adminActor(session.Principal.UserID), session.Principal.UserID, nil)
		return ErrPasswordInvalid
	}
	hashed, err := s.Hasher.Hash(ctx, next)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	now := s.now()
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET password_hash=$2,password_changed_at=$3,updated_at=$3 WHERE id=$1::uuid`, session.Principal.UserID, hashed, now); err != nil {
		return err
	}
	if err = revokeSessions(ctx, tx, session.Principal.UserID, "PASSWORD_CHANGED", now); err != nil {
		return err
	}
	if err = s.record(ctx, tx, "ADMIN_PASSWORD_CHANGED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"sessionsRevoked": true}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.Limiter.Reset(ctx, PolicyPassword, key)
	return nil
}

// RegenerateRecoveryCodes replaces every recovery code of the account.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, session *Session) ([]string, error) {
	if !session.MFAEnabled {
		return nil, ErrAccountState
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	codes, err := s.issueRecoveryCodes(ctx, tx, session.Principal.UserID)
	if err != nil {
		return nil, err
	}
	if err = s.record(ctx, tx, "ADMIN_RECOVERY_CODES_REGENERATED", adminActor(session.Principal.UserID), session.Principal.UserID, map[string]any{"recoveryCodes": len(codes)}); err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}

type activation struct{ tokenID, userID, email, name string }

func (s *Service) activation(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, token string, lock bool) (activation, error) {
	var a activation
	if !validToken(token) {
		return a, ErrActivationInvalid
	}
	query := `SELECT t.id::text,u.id::text,u.email,u.display_name FROM admin_activation_tokens t JOIN admin_users u ON u.id=t.admin_user_id
	 WHERE t.token_hash=$1 AND t.used_at IS NULL AND t.revoked_at IS NULL AND t.expires_at>$2 AND u.status='PENDING_ACTIVATION'`
	if lock {
		query += " FOR UPDATE OF t,u"
	}
	err := db.QueryRow(ctx, query, hashToken(token), s.now()).Scan(&a.tokenID, &a.userID, &a.email, &a.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrActivationInvalid
	}
	return a, err
}

func (s *Service) activationFailed(ctx context.Context, ipKey string) {
	s.fail(ctx, PolicyActivationIP, ipKey)
	s.recordNow(ctx, "ADMIN_ACTIVATION_FAILED", audit.Actor{Type: audit.ActorSystem}, "", map[string]any{"ipHash": ipKey})
}

// InspectActivation lets the activation page greet the account before a password is chosen.
func (s *Service) InspectActivation(ctx context.Context, token, ip string) (string, string, error) {
	ipKey := s.Keys.Pseudonym("ip", ip)
	if err := s.limit(ctx, PolicyActivationIP, ipKey); err != nil {
		return "", "", err
	}
	a, err := s.activation(ctx, s.Pool, token, false)
	if errors.Is(err, ErrActivationInvalid) {
		s.activationFailed(ctx, ipKey)
	}
	return a.email, a.name, err
}

// Activate sets the first password with a single-use, unexpired token.
func (s *Service) Activate(ctx context.Context, token, password, ip string) error {
	ipKey := s.Keys.Pseudonym("ip", ip)
	if err := s.limit(ctx, PolicyActivationIP, ipKey); err != nil {
		return err
	}
	a, err := s.activation(ctx, s.Pool, token, false)
	if errors.Is(err, ErrActivationInvalid) {
		s.activationFailed(ctx, ipKey)
	}
	if err != nil {
		return err
	}
	password = passwords.Normalize(password)
	if err = passwords.Check(password, a.email); err != nil {
		return passwordProblem("password", err.Error())
	}
	hashed, err := s.Hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if a, err = s.activation(ctx, tx, token, true); err != nil {
		return err
	}
	now := s.now()
	if _, err = tx.Exec(ctx, `UPDATE admin_activation_tokens SET used_at=$2 WHERE id=$1::uuid`, a.tokenID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_activation_tokens SET revoked_at=$2 WHERE admin_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, a.userID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_users SET password_hash=$2,status='ACTIVE',password_changed_at=$3,updated_at=$3 WHERE id=$1::uuid`, a.userID, hashed, now); err != nil {
		return err
	}
	if err = s.record(ctx, tx, "ADMIN_ACCOUNT_ACTIVATED", adminActor(a.userID), a.userID, map[string]any{"ipHash": ipKey}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
