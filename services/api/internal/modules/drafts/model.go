package drafts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"ifri-admissions/services/api/internal/modules/references"
)

const CookieName = "ifri_draft"
const CookiePath = "/api/v1/applications/drafts"

type Problem struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Status  int               `json:"-"`
}

func (p *Problem) Error() string { return p.Code }
func problem(status int, code, message string) *Problem {
	return &Problem{Code: code, Message: message, Status: status}
}

var (
	ErrSession    = problem(401, "DRAFT_SESSION_REQUIRED", "Aucun brouillon accessible sur ce navigateur.")
	ErrExpired    = problem(401, "DRAFT_SESSION_EXPIRED", "Votre session de brouillon a expiré. Vos données ne sont plus accessibles depuis ce navigateur.")
	ErrRevoked    = problem(401, "DRAFT_SESSION_REVOKED", "Cette session de brouillon a été révoquée.")
	ErrConflict   = problem(409, "DRAFT_CONFLICT", "Le brouillon a changé dans un autre onglet. Rechargez-le avant de continuer.")
	ErrClosed     = problem(409, "CAMPAIGN_NOT_OPEN", "Les candidatures ne sont actuellement pas ouvertes pour cette campagne.")
	ErrIneligible = problem(422, "APPLICATION_NOT_ELIGIBLE", "Cette filière n’est pas accessible avec votre qualification.")
	ErrLocked     = problem(409, "APPLICATION_LOCKED", "Votre candidature a été soumise : elle ne peut plus être modifiée.")
)

// Editable refuses every candidate operation except the two receipt reads once the
// application has left DRAFT; callers check it before the optimistic version.
func Editable(d Draft) error {
	if d.State != "DRAFT" {
		return ErrLocked
	}
	return nil
}

type Profile struct {
	LastName   string `json:"lastName"`
	FirstNames string `json:"firstNames"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
}
type Draft struct {
	ID                string                    `json:"id"`
	Profile           Profile                   `json:"profile"`
	Campaign          references.Campaign       `json:"campaign"`
	Qualification     *references.Qualification `json:"qualification"`
	GraduationYear    *int                      `json:"graduationYear"`
	Program           *references.Program       `json:"program"`
	State             string                    `json:"state"`
	ApplicationNumber *string                   `json:"applicationNumber"`
	SubmittedAt       *time.Time                `json:"submittedAt"`
	Version           int64                     `json:"version"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
	SessionExpiresAt  time.Time                 `json:"sessionExpiresAt"`
}
type Change struct {
	Version        int64    `json:"version"`
	Profile        *Profile `json:"profile,omitempty"`
	Qualification  string   `json:"qualification,omitempty"`
	GraduationYear *int     `json:"graduationYear,omitempty"`
	Program        string   `json:"program,omitempty"`
}

func NewSecret() (string, []byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, HashSecret(token), nil
}
func HashSecret(token string) []byte { hash := sha256.Sum256([]byte(token)); return hash[:] }
func ValidSecret(token string) bool {
	bytes, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(bytes) == 32 && !strings.Contains(token, "=")
}
