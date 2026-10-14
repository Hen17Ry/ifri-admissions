package submissions

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"ifri-admissions/services/api/internal/modules/analysis"
	"ifri-admissions/services/api/internal/modules/drafts"
)

// CalculationVersion identifies the averaging rule stored with each snapshot.
const CalculationVersion = 1

// AveragePlaces is the stored precision of the decimal average; the exact value is the fraction.
const AveragePlaces = 30

func problem(status int, code, message string) *drafts.Problem {
	return &drafts.Problem{Status: status, Code: code, Message: message}
}

var (
	ErrCampaignClosed    = problem(409, "CAMPAIGN_CLOSED", "Les candidatures sont closes pour cette campagne : la soumission n’est plus possible.")
	ErrAnalysisRequired  = problem(409, "TRANSCRIPT_ANALYSIS_REQUIRED", "Confirmez l’analyse de votre relevé de notes avant de soumettre votre candidature.")
	ErrAnalysisOutdated  = problem(409, "TRANSCRIPT_ANALYSIS_OUTDATED", "Votre relevé ou votre parcours a changé depuis votre confirmation. Confirmez l’analyse de votre relevé actuel.")
	ErrRulesChanged      = problem(409, "ADMISSION_RULES_CHANGED", "Les règles d’admission de votre filière ont changé. Votre relevé va être analysé de nouveau : vous devrez confirmer le résultat.")
	ErrVersionConflict   = problem(409, "VERSION_CONFLICT", "Votre dossier a changé dans un autre onglet. Rechargez-le avant de soumettre.")
	ErrNumbersExhausted  = problem(409, "APPLICATION_NUMBERS_EXHAUSTED", "Aucun numéro de candidature n’est disponible pour cette filière. Contactez l’institut.")
	ErrNotSubmitted      = problem(404, "SUBMISSION_NOT_FOUND", "Aucune candidature soumise n’est associée à cette session.")
	errSubjectsMismatch  = errors.New("extracted subjects differ from the expected subjects")
	numberPattern        = regexp.MustCompile(`^IFRI-[0-9]{4}-[A-Z][A-Z0-9_]{0,63}-[0-9]{6}$`)
	documentTypes        = []string{"REQUEST_LETTER", "TRANSCRIPT", "IDENTITY_DOCUMENT", "PARENTAL_COMMITMENT"}
	twenty               = big.NewRat(20, 1)
	errAverageImpossible = errors.New("average requires at least one subject with a positive coefficient")
)

// Receipt is everything the candidate sees after submission; it never contains the average.
type Receipt struct {
	ApplicationNumber string    `json:"applicationNumber"`
	SubmittedAt       time.Time `json:"submittedAt"`
	Candidate         struct {
		LastName   string `json:"lastName"`
		FirstNames string `json:"firstNames"`
	} `json:"candidate"`
	Campaign struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		AcademicYear string `json:"academicYear"`
	} `json:"campaign"`
	Program struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"program"`
	Qualification struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"qualification"`
	GraduationYear   int       `json:"graduationYear"`
	SessionExpiresAt time.Time `json:"sessionExpiresAt"`
}

// Outcome carries technical identifiers for logging; never sent to the candidate.
type Outcome struct {
	ApplicationID, SubmissionID string
	Replay                      bool
}

// Grade is one persisted (note, coefficient) pair, parsed exactly from NUMERIC text.
type Grade struct {
	Value, Coefficient *big.Rat
}

// Average is Σ(note × coeff) / Σ coeff. WeightedSum / CoefficientSum is the exact value; Decimal is
// that fraction truncated (never rounded) to AveragePlaces, Exact when nothing was truncated.
type Average struct {
	WeightedSum, CoefficientSum *big.Rat
	Decimal                     string
	Exact                       bool
}

// Fraction returns the exact average.
func (a Average) Fraction() *big.Rat { return new(big.Rat).Quo(a.WeightedSum, a.CoefficientSum) }

// ParseNumeric reads a PostgreSQL NUMERIC rendered as text ("15.50"), never through a float.
func ParseNumeric(raw string) (*big.Rat, error) {
	if raw == "" || strings.ContainsAny(raw, "eE/") {
		return nil, fmt.Errorf("numeric invalid")
	}
	v, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("numeric invalid")
	}
	return v, nil
}

// ComputeAverage applies the exact formula; grades must be in [0, 20] and coefficients > 0.
func ComputeAverage(grades []Grade) (Average, error) {
	if len(grades) == 0 {
		return Average{}, errAverageImpossible
	}
	weighted, coefficients := new(big.Rat), new(big.Rat)
	for _, g := range grades {
		if g.Value == nil || g.Coefficient == nil || g.Value.Sign() < 0 || g.Value.Cmp(twenty) > 0 || g.Coefficient.Sign() <= 0 {
			return Average{}, errAverageImpossible
		}
		weighted.Add(weighted, new(big.Rat).Mul(g.Value, g.Coefficient))
		coefficients.Add(coefficients, g.Coefficient)
	}
	average := Average{WeightedSum: weighted, CoefficientSum: coefficients}
	average.Decimal, average.Exact = Truncate(average.Fraction(), AveragePlaces)
	return average, nil
}

// Truncate renders a non-negative rational with exactly places decimals, cutting extra digits.
func Truncate(r *big.Rat, places int) (string, bool) {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	scaled := new(big.Int).Mul(r.Num(), scale)
	quotient, remainder := new(big.Int).QuoRem(scaled, r.Denom(), new(big.Int))
	digits := quotient.String()
	if places == 0 {
		return digits, remainder.Sign() == 0
	}
	if len(digits) <= places {
		digits = strings.Repeat("0", places-len(digits)+1) + digits
	}
	return digits[:len(digits)-places] + "." + digits[len(digits)-places:], remainder.Sign() == 0
}

// ExactDecimal renders a rational whose denominator has only the factors 2 and 5 (any sum or
// product of decimals) without loss; other rationals are refused rather than approximated.
func ExactDecimal(r *big.Rat) (string, error) {
	d := new(big.Int).Set(r.Denom())
	places := 0
	for _, f := range []int64{2, 5} {
		factor, count := big.NewInt(f), 0
		for new(big.Int).Mod(d, factor).Sign() == 0 {
			d.Quo(d, factor)
			count++
		}
		places = max(places, count)
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("rational has no finite decimal expansion")
	}
	text, exact := Truncate(r, places)
	if !exact {
		return "", fmt.Errorf("decimal expansion truncated")
	}
	return text, nil
}

// FormatNumber builds IFRI-{YEAR}-{PROGRAM}-{SERIAL6}.
func FormatNumber(year int, program string, serial int) (string, error) {
	number := fmt.Sprintf("IFRI-%04d-%s-%06d", year, program, serial)
	if serial < 1 || serial > 999999 || year < 2000 || year > 9999 || !numberPattern.MatchString(number) {
		return "", fmt.Errorf("application number invalid")
	}
	return number, nil
}

// Method states how a value was established, from the evidence persisted with the extraction.
// Only points and scale cells of the subject's own row can have been rebuilt from the Total.
func Method(e analysis.Evidence) (string, []string, error) {
	if e.Validation == "" {
		return "DIRECT_READ", []string{}, nil
	}
	if e.Validation != "CELL_ARITHMETIC" || e.ArithmeticRowIndex < 0 || e.ArithmeticRowIndex >= len(e.ArithmeticRows) {
		return "", nil, fmt.Errorf("evidence invalid")
	}
	cells := []string{}
	for _, cell := range e.ArithmeticRows[e.ArithmeticRowIndex].Reconstructed {
		if cell != "points" && cell != "scale" {
			return "", nil, fmt.Errorf("reconstructed cell invalid")
		}
		cells = append(cells, cell)
	}
	switch e.GradeSource {
	case "DIRECT_CELL":
		return "ROW_ARITHMETIC", cells, nil
	case "POINTS_TOTALS":
		return "TABLE_TOTALS", cells, nil
	}
	return "", nil, fmt.Errorf("grade source invalid")
}
