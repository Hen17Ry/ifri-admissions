package documents

import (
	"crypto/rand"
	"fmt"
	"ifri-admissions/services/api/internal/modules/drafts"
	"time"
)

type Type struct {
	Code     string `json:"type"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

var Types = []Type{{"REQUEST_LETTER", "Demande adressée au Directeur de l’IFRI", true}, {"TRANSCRIPT", "Relevé de notes BAC ou DT", true}, {"IDENTITY_DOCUMENT", "Carte d’identité ou CIP", true}, {"PARENTAL_COMMITMENT", "Engagement parental", true}}

func Known(code string) bool {
	for _, t := range Types {
		if t.Code == code {
			return true
		}
	}
	return false
}

type Document struct {
	Type
	Uploaded  bool       `json:"uploaded"`
	Version   int        `json:"version"`
	Filename  string     `json:"filename"`
	Size      int64      `json:"sizeBytes"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}
type Listing struct {
	Documents          []Document `json:"documents"`
	Complete           bool       `json:"complete"`
	MaxBytes           int64      `json:"maxBytes"`
	ApplicationVersion int64      `json:"applicationVersion"`
}
type Version struct {
	ID, DocumentID, Key, Filename, SHA256 string
	Number                                int
	Size                                  int64
	CreatedAt                             time.Time
}

func newUUID() (string, error) {
	v := make([]byte, 16)
	if _, err := rand.Read(v); err != nil {
		return "", err
	}
	v[6] = (v[6] & 15) | 64
	v[8] = (v[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", v[:4], v[4:6], v[6:8], v[8:10], v[10:]), nil
}
func issue(status int, code, message string) error {
	return &drafts.Problem{Status: status, Code: code, Message: message}
}
