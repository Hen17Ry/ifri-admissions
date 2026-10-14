package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"
)

const (
	CookieName = "ifri_admin"
	BasePath   = "/api/v1/admin"
	CSRFHeader = "X-CSRF-Token"
	// A session between the password and the second factor lives at most this long.
	preAuthLifetime = 10 * time.Minute
	recoveryCodes   = 10
)

const (
	StageEnrollment   = "MFA_ENROLLMENT"
	StageVerification = "MFA_VERIFICATION"
	StageActive       = "ACTIVE"
)

// Problem is the API error format shared with the candidate routes.
type Problem struct {
	Status     int               `json:"-"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Fields     map[string]string `json:"fields,omitempty"`
	RetryAfter time.Duration     `json:"-"`
}

func (p *Problem) Error() string { return p.Code }

func problem(status int, code, message string) *Problem {
	return &Problem{Status: status, Code: code, Message: message}
}

var (
	// One message for every login failure: unknown, disabled, pending or wrong password.
	ErrInvalidCredentials = problem(401, "ADMIN_LOGIN_FAILED", "Identifiant ou mot de passe incorrect.")
	ErrUnauthenticated    = problem(401, "ADMIN_SESSION_REQUIRED", "Connectez-vous pour accéder à l’espace d’administration.")
	ErrSessionExpired     = problem(401, "ADMIN_SESSION_EXPIRED", "Votre session a expiré. Connectez-vous de nouveau.")
	ErrStage              = problem(403, "ADMIN_STAGE_FORBIDDEN", "Terminez d’abord la vérification en deux étapes.")
	ErrForbidden          = problem(403, "ADMIN_FORBIDDEN", "Vous n’avez pas l’autorisation d’effectuer cette action.")
	ErrOrigin             = problem(403, "ORIGIN_FORBIDDEN", "Cette origine n’est pas autorisée.")
	ErrCSRF               = problem(403, "ADMIN_CSRF_INVALID", "La requête n’a pas pu être vérifiée. Rechargez la page.")
	ErrJSON               = problem(415, "JSON_REQUIRED", "Une requête JSON est requise.")
	ErrInvalid            = problem(400, "INVALID_PARAMETERS", "Le contenu de la requête est invalide.")
	// 422, not 401: a mistyped code or password must not end the session being completed.
	ErrMFAInvalid        = &Problem{Status: 422, Code: "ADMIN_MFA_INVALID", Message: "Code incorrect ou déjà utilisé.", Fields: map[string]string{"code": "Code incorrect ou déjà utilisé."}}
	ErrActivationInvalid = problem(410, "ADMIN_ACTIVATION_INVALID", "Ce lien d’activation n’est plus valide. Demandez-en un nouveau à un super-administrateur.")
	ErrPasswordInvalid   = &Problem{Status: 422, Code: "ADMIN_PASSWORD_INVALID", Message: "Mot de passe actuel incorrect.", Fields: map[string]string{"currentPassword": "Mot de passe actuel incorrect."}}
	ErrNotFound          = problem(404, "ADMIN_ACCOUNT_NOT_FOUND", "Ce compte est introuvable.")
	ErrAccountExists     = problem(409, "ADMIN_ACCOUNT_EXISTS", "Un compte existe déjà avec cette adresse email.")
	ErrLastSuperAdmin    = problem(409, "ADMIN_LAST_SUPER_ADMIN", "Il doit rester au moins un super-administrateur actif.")
	ErrSelfAction        = problem(409, "ADMIN_SELF_ACTION_FORBIDDEN", "Vous ne pouvez pas désactiver votre propre compte.")
	ErrAccountState      = problem(409, "ADMIN_ACCOUNT_STATE", "Cette action n’est pas possible dans l’état actuel du compte.")
	ErrUnavailable       = problem(503, "ADMIN_UNAVAILABLE", "Le service est momentanément indisponible. Réessayez.")
)

func rateLimited(retry time.Duration) *Problem {
	p := problem(429, "ADMIN_RATE_LIMITED", "Trop de tentatives. Réessayez dans quelques minutes.")
	p.RetryAfter = retry
	return p
}

// passwordProblem maps a policy refusal to a field error.
func passwordProblem(field, code string) *Problem {
	messages := map[string]string{
		"PASSWORD_TOO_SHORT":       "Choisissez au moins 12 caractères. Une phrase de plusieurs mots est plus facile à retenir.",
		"PASSWORD_TOO_LONG":        "Choisissez au plus 128 caractères.",
		"PASSWORD_TOO_COMMON":      "Ce mot de passe figure parmi les plus utilisés. Choisissez-en un autre.",
		"PASSWORD_MATCHES_ACCOUNT": "Le mot de passe ne doit pas reprendre votre adresse email.",
	}
	message, ok := messages[code]
	if !ok {
		message = "Ce mot de passe n’est pas accepté."
	}
	return &Problem{Status: 422, Code: code, Message: message, Fields: map[string]string{field: message}}
}

// newToken returns a 256-bit URL-safe token and its SHA-256 hash (only the hash is stored).
func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32
}

// Recovery codes: 10 Crockford base32 characters (50 bits), shown as XXXXX-XXXXX.
const recoveryAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newRecoveryCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, 0, 11)
	for i, b := range raw {
		if i == 5 {
			out = append(out, '-')
		}
		out = append(out, recoveryAlphabet[b&31])
	}
	return string(out), nil
}

func normalizeRecoveryCode(input string) string {
	r := strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1")
	return r.Replace(strings.ToUpper(strings.TrimSpace(input)))
}

func looksLikeRecoveryCode(input string) bool { return len(normalizeRecoveryCode(input)) == 10 }

func hashRecoveryCode(input string) []byte {
	sum := sha256.Sum256([]byte("ifri-admin-recovery:" + normalizeRecoveryCode(input)))
	return sum[:]
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Account is the administration view of an account; no credential ever leaves the server.
type Account struct {
	ID                string     `json:"id"`
	Email             string     `json:"email"`
	DisplayName       string     `json:"displayName"`
	Status            string     `json:"status"`
	MFAEnabled        bool       `json:"mfaEnabled"`
	LastLoginAt       *time.Time `json:"lastLoginAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	ActivationExpires *time.Time `json:"activationExpiresAt"`
	Grants            []Grant    `json:"grants"`
}

// SessionView is what the interface learns about its own session.
type SessionView struct {
	ID          string       `json:"id"`
	Stage       string       `json:"stage"`
	CSRFToken   string       `json:"csrfToken"`
	Email       string       `json:"email"`
	DisplayName string       `json:"displayName"`
	Permissions []Permission `json:"permissions"`
	Grants      []Grant      `json:"grants"`
	IdleExpires time.Time    `json:"idleExpiresAt"`
	Expires     time.Time    `json:"expiresAt"`
	MFARequired bool         `json:"mfaRequired"`
}
