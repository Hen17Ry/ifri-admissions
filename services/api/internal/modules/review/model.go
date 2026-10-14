// Package review is the back-office of submitted applications: list, dossier, documents, OCR
// proofs, review workflow and administrative corrections. The submission snapshot and
// applications.state never change; reviews, history and corrections live beside them.
package review

import (
	"encoding/json"
	"time"

	"ifri-admissions/services/api/internal/modules/admin"
)

const (
	StatusToReview  = "TO_REVIEW"
	StatusInReview  = "IN_REVIEW"
	StatusValidated = "VALIDATED"
	StatusRejected  = "REJECTED"
)

var (
	ErrNotFound          = admin.NewProblem(404, "APPLICATION_NOT_FOUND", "Ce dossier est introuvable.")
	ErrVersion           = admin.NewProblem(409, "REVIEW_VERSION_CONFLICT", "Le dossier vient d’être modifié par une autre action. Rechargez-le avant de continuer.")
	ErrFrozen            = admin.NewProblem(409, "REVIEW_FROZEN", "Le classement de cette filière est gelé : le dossier ne peut plus être modifié.")
	ErrNotAssignee       = admin.NewProblem(403, "REVIEW_NOT_ASSIGNEE", "Seul l’agent qui a pris ce dossier en charge peut effectuer cette action.")
	ErrTransition        = admin.NewProblem(409, "REVIEW_TRANSITION_INVALID", "Cette action n’est pas possible dans l’état actuel du dossier.")
	ErrPendingCorrection = admin.NewProblem(409, "REVIEW_CORRECTION_PENDING", "Une correction attend l’avis du directeur adjoint : le dossier ne peut pas être validé.")
	ErrCorrectionMissing = admin.NewProblem(404, "CORRECTION_NOT_FOUND", "Cette correction est introuvable.")
	ErrCorrectionState   = admin.NewProblem(409, "CORRECTION_ALREADY_DECIDED", "Cette correction a déjà été traitée.")
	ErrFourEyes          = admin.NewProblem(403, "CORRECTION_FOUR_EYES", "Une correction ne peut pas être approuvée par son auteur.")
	ErrDocument          = admin.NewProblem(404, "DOCUMENT_NOT_FOUND", "Ce document est introuvable.")
	ErrStorage           = admin.NewProblem(503, "DOCUMENT_UNAVAILABLE", "Le document est momentanément indisponible.")
	ErrProofNotReady     = admin.NewProblem(409, "PROOF_NOT_READY", "La preuve n’est pas encore disponible.")
)

func invalid(fields map[string]string) *admin.Problem {
	p := admin.NewProblem(422, "VALIDATION_FAILED", "Vérifiez les informations saisies.")
	p.Fields = fields
	return p
}

// Person is an administrator as shown in the back-office.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Average struct {
	// Display is truncated to 2 decimals for reading; the exact fraction stays the reference.
	Display        string `json:"display"`
	Decimal        string `json:"decimal"`
	Exact          bool   `json:"exact"`
	WeightedSum    string `json:"weightedSum"`
	CoefficientSum string `json:"coefficientSum"`
}

type ListItem struct {
	ID                 string   `json:"id"`
	Number             string   `json:"applicationNumber"`
	SubmittedAt        string   `json:"submittedAt"`
	LastName           string   `json:"lastName"`
	FirstNames         string   `json:"firstNames"`
	Qualification      string   `json:"qualification"`
	QualificationLabel string   `json:"qualificationLabel"`
	Program            string   `json:"program"`
	ProgramName        string   `json:"programName"`
	Average            *Average `json:"average"`
	Status             string   `json:"status"`
	Assignee           *Person  `json:"assignee"`
	Flags              []string `json:"flags"`
	PendingCorrections int      `json:"pendingCorrections"`
}

type ListPage struct {
	Items    []ListItem `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

// Filter of the dossier list; empty values mean "any".
type Filter struct {
	Query, Program, Qualification, Status, Flag, Sort, Direction string
	Assignee                                                     string // "", "none", "me" or an account id
	PendingCorrection                                            bool
	Page, PageSize                                               int
}

type DocumentView struct {
	Type      string `json:"type"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"sizeBytes"`
	Version   int    `json:"version"`
}

type CorrectionView struct {
	ID             string     `json:"id"`
	ApplicationID  string     `json:"applicationId"`
	Number         string     `json:"applicationNumber,omitempty"`
	Candidate      string     `json:"candidate,omitempty"`
	SubjectID      string     `json:"subjectId"`
	SubjectName    string     `json:"subjectName"`
	Field          string     `json:"field"`
	OriginalValue  string     `json:"originalValue"`
	CurrentValue   string     `json:"currentValue"`
	ProposedValue  string     `json:"proposedValue"`
	Reason         string     `json:"reason"`
	ProposedBy     Person     `json:"proposedBy"`
	ProposedAt     time.Time  `json:"proposedAt"`
	Status         string     `json:"status"`
	DecidedBy      *Person    `json:"decidedBy"`
	DecidedAt      *time.Time `json:"decidedAt"`
	DecisionReason string     `json:"decisionReason,omitempty"`
	ReviewVersion  int        `json:"reviewVersion,omitempty"`
}

type SubjectView struct {
	SubjectID            string   `json:"subjectId"`
	Code                 string   `json:"code"`
	Name                 string   `json:"name"`
	Position             int      `json:"position"`
	GradeRaw             string   `json:"gradeRaw"`
	Grade                string   `json:"grade"`
	CoefficientRaw       string   `json:"coefficientRaw"`
	Coefficient          string   `json:"coefficient"`
	EffectiveGrade       string   `json:"effectiveGrade"`
	EffectiveCoefficient string   `json:"effectiveCoefficient"`
	Method               string   `json:"validationMethod"`
	Reconstructed        []string `json:"reconstructedCells"`
	ProofStatus          string   `json:"proofStatus"`
}

type EventView struct {
	Version      int       `json:"version"`
	Type         string    `json:"type"`
	Actor        *Person   `json:"actor"`
	FromStatus   string    `json:"fromStatus,omitempty"`
	ToStatus     string    `json:"toStatus"`
	FromAssignee *Person   `json:"fromAssignee,omitempty"`
	ToAssignee   *Person   `json:"toAssignee,omitempty"`
	ReasonCode   string    `json:"reasonCode,omitempty"`
	Comment      string    `json:"comment,omitempty"`
	CorrectionID string    `json:"correctionId,omitempty"`
	OccurredAt   time.Time `json:"occurredAt"`
}

// FlagView is one duplicate signal, seen from this dossier in either direction.
type FlagView struct {
	Code          string `json:"code"`
	Direction     string `json:"direction"` // OUTGOING: this dossier carries it; INCOMING: another one does
	ApplicationID string `json:"applicationId,omitempty"`
	Number        string `json:"applicationNumber,omitempty"`
}

type Detail struct {
	ID                 string           `json:"id"`
	Number             string           `json:"applicationNumber"`
	SubmittedAt        time.Time        `json:"submittedAt"`
	Campaign           string           `json:"campaign"`
	LastName           string           `json:"lastName"`
	FirstNames         string           `json:"firstNames"`
	Email              string           `json:"email"`
	Phone              string           `json:"phone"`
	Qualification      string           `json:"qualification"`
	QualificationLabel string           `json:"qualificationLabel"`
	GraduationYear     int              `json:"graduationYear"`
	Program            string           `json:"program"`
	ProgramName        string           `json:"programName"`
	DeclaredSeries     string           `json:"declaredSeries"`
	ReadSeries         string           `json:"readSeries"`
	SnapshotAverage    *Average         `json:"snapshotAverage"`
	EffectiveAverage   *Average         `json:"effectiveAverage"`
	Status             string           `json:"status"`
	Assignee           *Person          `json:"assignee"`
	Version            int              `json:"version"`
	RejectionCode      string           `json:"rejectionCode,omitempty"`
	RejectionLabel     string           `json:"rejectionLabel,omitempty"`
	RejectionComment   string           `json:"rejectionComment,omitempty"`
	Frozen             bool             `json:"frozen"`
	Documents          []DocumentView   `json:"documents"`
	Subjects           []SubjectView    `json:"subjects"`
	Corrections        []CorrectionView `json:"corrections"`
	History            []EventView      `json:"history"`
	Flags              []FlagView       `json:"flags"`
}

type Counter struct {
	Program     string `json:"program"`
	ProgramName string `json:"programName"`
	Status      string `json:"status"`
	Count       int    `json:"count"`
}

type Dashboard struct {
	Counters           []Counter       `json:"counters,omitempty"`
	PendingCorrections *int            `json:"pendingCorrections,omitempty"`
	Unassigned         *int            `json:"unassigned,omitempty"`
	Mine               *int            `json:"mine,omitempty"`
	Coverage           *admin.Coverage `json:"coverage,omitempty"`
}

type Reason struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type Reference struct {
	RejectionReasons []Reason `json:"rejectionReasons"`
	Agents           []Person `json:"agents"`
	Programs         []Reason `json:"programs"`
	Qualifications   []Reason `json:"qualifications"`
}

// proofJob is the contract v1 of the proof stream read by the OCR worker.
type proofJob struct {
	ContractVersion int             `json:"contractVersion"`
	ProofID         string          `json:"proofId"`
	AnalysisID      string          `json:"analysisId"`
	DocumentVersion string          `json:"documentVersionId"`
	DocumentSHA256  string          `json:"documentSha256"`
	Page            int             `json:"pageNumber"`
	Evidence        json.RawMessage `json:"evidence"`
}
