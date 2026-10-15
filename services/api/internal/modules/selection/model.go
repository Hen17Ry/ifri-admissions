// Package selection ranks the validated dossiers of a program, freezes the ranking into an
// immutable snapshot, records withdrawals and promotions as append-only events and exports the
// public and internal lists. Contract: docs/RANKING_SELECTION.md.
package selection

import (
	"strings"
	"time"

	"ifri-admissions/services/api/internal/modules/admin"
)

var (
	ErrOfferNotFound   = admin.NewProblem(404, "OFFER_NOT_FOUND", "Cette filière est introuvable pour cette campagne.")
	ErrFrozen          = admin.NewProblem(409, "RANKING_FROZEN", "Le classement de cette filière est gelé : il ne peut plus changer.")
	ErrNotFrozen       = admin.NewProblem(409, "RANKING_NOT_FROZEN", "Le classement n’est pas encore gelé : la sélection et les exports viennent après le gel.")
	ErrSettingsChanged = admin.NewProblem(409, "RANKING_SETTINGS_CHANGED", "Le nombre de places ou la moyenne minimale vient de changer. Rechargez avant de geler.")
	ErrVersion         = admin.NewProblem(409, "SELECTION_VERSION_CONFLICT", "La liste vient d’être modifiée par une autre action. Rechargez-la avant de continuer.")
	ErrEntryNotFound   = admin.NewProblem(404, "CANDIDATE_NOT_RANKED", "Ce candidat ne figure pas dans le classement gelé.")
	ErrState           = admin.NewProblem(409, "SELECTION_STATE_INVALID", "Cette action n’est pas possible pour ce candidat dans l’état actuel de la liste.")
	ErrNoFreeSeat      = admin.NewProblem(409, "SELECTION_NO_FREE_SEAT", "Aucune place n’est libre : une promotion suit toujours un désistement d’admis.")
	ErrOrder           = admin.NewProblem(409, "SELECTION_ORDER", "Les promotions suivent strictement l’ordre de la liste d’attente : enregistrez d’abord le désistement des candidats qui précèdent.")
	ErrExportNotFound  = admin.NewProblem(404, "EXPORT_NOT_FOUND", "Cet export est introuvable.")
	ErrExportNotReady  = admin.NewProblem(409, "EXPORT_NOT_READY", "Le document n’est pas encore prêt.")
	ErrStorage         = admin.NewProblem(503, "EXPORT_UNAVAILABLE", "Le document est momentanément indisponible.")
)

func invalid(fields map[string]string) *admin.Problem {
	p := admin.NewProblem(422, "VALIDATION_FAILED", "Vérifiez les informations saisies.")
	p.Fields = fields
	return p
}

// blocked lists every unmet precondition of a freeze (field = code, value = message).
func blocked(blockers []Blocker) *admin.Problem {
	p := admin.NewProblem(409, "RANKING_FREEZE_BLOCKED", "Le gel est impossible tant que toutes les conditions ne sont pas réunies.")
	p.Fields = map[string]string{}
	for _, b := range blockers {
		p.Fields[b.Code] = b.Message
	}
	return p
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Offer is one program of one campaign, with the Director's settings.
type Offer struct {
	ID             string    `json:"id"`
	CampaignCode   string    `json:"campaignCode"`
	CampaignName   string    `json:"campaignName"`
	ProgramCode    string    `json:"programCode"`
	ProgramName    string    `json:"programName"`
	Seats          *int      `json:"seats"`
	MinimumAverage string    `json:"minimumAverage"`
	CampaignOpen   bool      `json:"campaignOpen"`
	EndsAt         time.Time `json:"endsAt"`
	Frozen         bool      `json:"frozen"`
}

type Average struct {
	Display        string `json:"display"`
	Decimal        string `json:"decimal"`
	Exact          bool   `json:"exact"`
	WeightedSum    string `json:"weightedSum"`
	CoefficientSum string `json:"coefficientSum"`
}

// Entry is one candidate of the ranking (provisional or frozen) as shown to the administration.
type Entry struct {
	ApplicationID     string    `json:"applicationId"`
	Number            string    `json:"applicationNumber"`
	LastName          string    `json:"lastName"`
	FirstNames        string    `json:"firstNames"`
	Qualification     string    `json:"qualification"`
	Position          int       `json:"position"`
	Rank              *int      `json:"rank"`
	Average           Average   `json:"average"`
	MathSubject       string    `json:"mathSubject,omitempty"`
	MathGrade         string    `json:"mathGrade,omitempty"`
	MathTiebreak      string    `json:"mathTiebreak"`
	SubmittedAt       time.Time `json:"submittedAt"`
	DecisiveCriterion string    `json:"decisiveCriterion,omitempty"`
	InitialDecision   string    `json:"initialDecision,omitempty"`
	Decision          string    `json:"decision,omitempty"`
}

type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
}

type Counts struct {
	Validated          int `json:"validated"`
	Rejected           int `json:"rejected"`
	ToReview           int `json:"toReview"`
	InReview           int `json:"inReview"`
	PendingCorrections int `json:"pendingCorrections"`
}

type Freeze struct {
	FrozenAt       time.Time `json:"frozenAt"`
	FrozenBy       Person    `json:"frozenBy"`
	Seats          int       `json:"seats"`
	MinimumAverage string    `json:"minimumAverage"`
	ListVersion    int       `json:"listVersion"`
	VersionAt      time.Time `json:"versionAt"`
	FreeSeats      int       `json:"freeSeats"`
}

type EventView struct {
	Version       int       `json:"version"`
	Type          string    `json:"type"`
	ApplicationID string    `json:"applicationId"`
	Number        string    `json:"applicationNumber"`
	Candidate     string    `json:"candidate"`
	Reason        string    `json:"reason,omitempty"`
	Actor         Person    `json:"actor"`
	OccurredAt    time.Time `json:"occurredAt"`
}

type ExportView struct {
	ID          string    `json:"id"`
	ListVersion int       `json:"listVersion"`
	Variant     string    `json:"variant"`
	Status      string    `json:"status"`
	FailureCode string    `json:"failureCode,omitempty"`
	RequestedBy Person    `json:"requestedBy"`
	RequestedAt time.Time `json:"requestedAt"`
}

// View is the ranking page: provisional (computed on demand) or frozen (snapshot + events).
type View struct {
	Offer    Offer        `json:"offer"`
	Counts   Counts       `json:"counts"`
	Blockers []Blocker    `json:"blockers"`
	Freeze   *Freeze      `json:"freeze"`
	Entries  []Entry      `json:"entries"`
	Events   []EventView  `json:"events"`
	Proposal *Entry       `json:"proposal"`
	Exports  []ExportView `json:"exports"`
}

// French labels of the exports (the interface keeps its own copy of the same codes).
var decisionLabels = map[string]string{Admitted: "Admis", Waitlisted: "Liste d’attente", Withdrawn: "Désisté", NotRanked: "Non classé"}

var criterionLabels = map[string]string{
	CriterionAverage: "Moyenne", CriterionMath: "Note de mathématiques", CriterionDate: "Date de soumission",
	CriterionNumber: "Numéro de candidature",
}

var mathLabels = map[string]string{
	MathNotNeeded: "Sans objet (moyenne unique)", MathCompared: "Comparées",
	MathSkipped: "Maths non comparées : candidat sans matière mathématique dans le groupe",
}

// frenchNumber renders an exact decimal with a decimal comma.
func frenchNumber(value string) string { return strings.Replace(value, ".", ",", 1) }

var beninTime = func() *time.Location {
	if loc, err := time.LoadLocation("Africa/Porto-Novo"); err == nil {
		return loc
	}
	return time.FixedZone("WAT", 3600)
}()

func frenchDate(t time.Time) string { return t.In(beninTime).Format("02/01/2006 15:04") }
