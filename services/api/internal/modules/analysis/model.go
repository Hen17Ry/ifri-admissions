package analysis

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"regexp"
	"strings"
)

type Expected struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}
type BBox struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}
type ArithmeticRow struct {
	Grade       string `json:"grade"`
	Coefficient string `json:"coefficient"`
	Points      string `json:"points"`
	Scale       string `json:"scale"`
}
type Evidence struct {
	Points             *BBox           `json:"points,omitempty"`
	Scale              *BBox           `json:"scale,omitempty"`
	GradeSource        string          `json:"gradeSource,omitempty"`
	Validation         string          `json:"validation,omitempty"`
	ArithmeticRows     []ArithmeticRow `json:"arithmeticRows,omitempty"`
	ArithmeticRowIndex int             `json:"arithmeticRowIndex,omitempty"`
	TotalCells         []string        `json:"totalCells,omitempty"`
	Series             string          `json:"series,omitempty"`

	Profile       string  `json:"profile,omitempty"`
	Section       string  `json:"section,omitempty"`
	Preprocessing string  `json:"preprocessing,omitempty"`
	DPI           int     `json:"dpi,omitempty"`
	PSM           int     `json:"psm,omitempty"`
	DeskewDegrees float64 `json:"deskewDegrees,omitempty"`
	Subject       BBox    `json:"subject"`
	Grade         BBox    `json:"grade"`
	Coefficient   BBox    `json:"coefficient"`
	System        string  `json:"system"`
}
type Extraction struct {
	SubjectID             string   `json:"subjectId"`
	Position              int      `json:"position"`
	Detected              string   `json:"detectedSubjectText"`
	GradeRaw              string   `json:"gradeRaw"`
	Grade                 string   `json:"gradeValue"`
	CoefficientRaw        string   `json:"coefficientRaw"`
	Coefficient           string   `json:"coefficientValue"`
	Page                  int      `json:"pageNumber"`
	SubjectConfidence     float64  `json:"subjectConfidence"`
	GradeConfidence       float64  `json:"gradeConfidence"`
	CoefficientConfidence float64  `json:"coefficientConfidence"`
	Confidence            float64  `json:"overallConfidence"`
	Evidence              Evidence `json:"evidence"`
}
type Result struct {
	Pages           int          `json:"pageCount,omitempty"`
	ContractVersion int          `json:"contractVersion"`
	Attempt         int          `json:"attemptNumber"`
	Status          string       `json:"status"`
	Engine          string       `json:"engine"`
	EngineVersion   string       `json:"engineVersion"`
	Failure         string       `json:"failureCode"`
	Subjects        []Extraction `json:"subjects"`
}

var decimalPattern = regexp.MustCompile(`^[0-9]{1,64}([.,][0-9]{1,32})?$`)

func decimal(raw string) (*big.Rat, error) {
	raw = strings.TrimSpace(raw)
	raw = regexp.MustCompile(`\s+([.,])`).ReplaceAllString(raw, "$1")
	raw = strings.TrimSuffix(strings.TrimSuffix(raw, "."), ",")
	if !decimalPattern.MatchString(raw) {
		return nil, errors.New("decimal invalid")
	}
	v, ok := new(big.Rat).SetString(strings.ReplaceAll(raw, ",", "."))
	if !ok {
		return nil, errors.New("decimal invalid")
	}
	return v, nil
}
func validBox(b BBox) bool {
	for _, v := range []float64{b.X0, b.Y0, b.X1, b.Y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	return b.X1 > b.X0 && b.Y1 > b.Y0
}
func validScanEvidence(e Evidence) bool {
	if e.Profile != "" && e.Profile != "UNKNOWN_GENERIC" && e.Profile != "BENIN_BAC_GENERAL" {
		return false
	}
	if e.Section != "" && e.Section != "GENERIC_TABLE" && e.Section != "FIRST_GROUP_WRITTEN" {
		return false
	}
	if e.Preprocessing != "" && e.Preprocessing != "clahe" && e.Preprocessing != "adaptive" && e.Preprocessing != "color-channel" && e.Preprocessing != "hsv-v" {
		return false
	}
	if e.DPI != 0 && (e.DPI < 250 || e.DPI > 400) {
		return false
	}
	if e.PSM != 0 && e.PSM != 6 && e.PSM != 11 && e.PSM != 7 {
		return false
	}
	return !math.IsNaN(e.DeskewDegrees) && !math.IsInf(e.DeskewDegrees, 0) && math.Abs(e.DeskewDegrees) <= 5
}
func Validate(result Result, expected []Expected, threshold float64) error {
	if result.Pages < 0 || result.Pages > 100 {
		return errors.New("page count invalid")
	}
	if result.ContractVersion != 1 {
		return errors.New("contract version invalid")
	}
	if result.Status != "SUCCEEDED" && result.Status != "FAILED" && result.Status != "LOW_CONFIDENCE" {
		return errors.New("status invalid")
	}
	if strings.TrimSpace(result.Engine) == "" || strings.TrimSpace(result.EngineVersion) == "" || len(result.Engine) > 100 || len(result.EngineVersion) > 100 {
		return errors.New("engine invalid")
	}
	failures := map[string]bool{"COLUMN_SEGMENTATION_FAILED": true, "ROW_SEGMENTATION_FAILED": true, "NEEDS_REVIEW": true, "PDF_UNREADABLE": true, "NO_TEXT_FOUND": true, "SUBJECT_NOT_FOUND": true, "GRADE_NOT_FOUND": true, "COEFFICIENT_NOT_FOUND": true, "AMBIGUOUS_ROW": true, "LOW_CONFIDENCE": true, "OCR_ENGINE_ERROR": true, "ANALYSIS_TIMEOUT": true}
	if result.Status != "SUCCEEDED" {
		if !failures[result.Failure] || len(result.Subjects) != 0 {
			return errors.New("failure invalid")
		}
		return nil
	}
	if result.Failure != "" || len(expected) == 0 || len(result.Subjects) != len(expected) {
		return errors.New("incomplete")
	}
	seen := map[string]bool{}
	for _, s := range result.Subjects {
		found := false
		for _, e := range expected {
			if e.ID == s.SubjectID && e.Position == s.Position {
				found = true
			}
		}
		if !found || seen[s.SubjectID] || s.Page < 1 || s.Page > 100 || len(s.Detected) == 0 || len(s.Detected) > 300 || len(s.GradeRaw) > 32 || len(s.CoefficientRaw) > 32 {
			return errors.New("subject invalid")
		}
		seen[s.SubjectID] = true
		g, err := decimal(s.Grade)
		if err != nil || g.Sign() < 0 || g.Cmp(big.NewRat(20, 1)) > 0 {
			return errors.New("grade invalid")
		}
		c, err := decimal(s.Coefficient)
		if err != nil || c.Sign() <= 0 {
			return errors.New("coefficient invalid")
		}
		rg, err := decimal(s.GradeRaw)
		if s.Evidence.GradeSource != "POINTS_TOTALS" && (err != nil || rg.Cmp(g) != 0) {
			return errors.New("grade evidence invalid")
		}
		rc, err := decimal(s.CoefficientRaw)
		if err != nil || rc.Cmp(c) != 0 {
			return errors.New("coefficient evidence invalid")
		}
		if s.Evidence.Preprocessing == "hsv-v" && s.Evidence.Validation != "CELL_ARITHMETIC" {
			return errors.New("cell arithmetic required")
		}
		if s.Evidence.GradeSource == "POINTS_TOTALS" && s.Evidence.Validation != "CELL_ARITHMETIC" {
			return errors.New("derived grade proof required")
		}
		confidenceFloor := threshold
		if s.Evidence.Validation != "" {
			if s.Evidence.Validation != "CELL_ARITHMETIC" || !validArithmetic(s) {
				return errors.New("arithmetic evidence invalid")
			}
			confidenceFloor = 0
		}
		for _, v := range []float64{s.SubjectConfidence, s.GradeConfidence, s.CoefficientConfidence, s.Confidence} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < confidenceFloor || v > 1 {
				return errors.New("confidence invalid")
			}
		}
		if !validScanEvidence(s.Evidence) || s.Evidence.System != "NORMALIZED_TOP_LEFT" || !validBox(s.Evidence.Subject) || !validBox(s.Evidence.Grade) || !validBox(s.Evidence.Coefficient) {
			return errors.New("evidence invalid")
		}
	}
	return nil
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }

// Independent rational checks: low OCR confidence never bypasses cell arithmetic.
func validArithmetic(s Extraction) bool {
	e := s.Evidence
	if e.GradeSource == "POINTS_TOTALS" && (e.Points == nil || e.Scale == nil || !validBox(*e.Points) || !validBox(*e.Scale)) {
		return false
	}
	if e.GradeSource != "" && e.GradeSource != "DIRECT_CELL" && e.GradeSource != "POINTS_TOTALS" {
		return false
	}
	if e.Profile != "BENIN_BAC_GENERAL" || e.Section != "FIRST_GROUP_WRITTEN" || e.Preprocessing != "hsv-v" || e.PSM != 7 || !strings.Contains("ABCDE", e.Series) || len(e.Series) != 1 || len(e.ArithmeticRows) < 1 || len(e.ArithmeticRows) > 30 || len(e.TotalCells) != 4 || e.ArithmeticRowIndex < 0 || e.ArithmeticRowIndex >= len(e.ArithmeticRows) {
		return false
	}
	sums := []*big.Rat{new(big.Rat), new(big.Rat), new(big.Rat)}
	for _, row := range e.ArithmeticRows {
		g, eg := decimal(row.Grade)
		c, ec := decimal(row.Coefficient)
		p, ep := decimal(row.Points)
		scale, es := decimal(row.Scale)
		if e.GradeSource == "POINTS_TOTALS" && row.Points == "" {
			return false
		}
		if eg != nil || ec != nil || (row.Points != "" && ep != nil) || es != nil || g.Cmp(big.NewRat(20, 1)) > 0 || c.Sign() <= 0 || !c.IsInt() || c.Cmp(big.NewRat(6, 1)) > 0 || (row.Points != "" && new(big.Rat).Mul(g, c).Cmp(p) != 0) || new(big.Rat).Mul(big.NewRat(20, 1), c).Cmp(scale) != 0 {
			return false
		}
		sums[0].Add(sums[0], c)
		sums[1].Add(sums[1], new(big.Rat).Mul(g, c))
		sums[2].Add(sums[2], scale)
	}
	selected := e.ArithmeticRows[e.ArithmeticRowIndex]
	g, _ := decimal(s.Grade)
	c, _ := decimal(s.Coefficient)
	rg, _ := decimal(selected.Grade)
	rc, _ := decimal(selected.Coefficient)
	if g == nil || c == nil || g.Cmp(rg) != 0 || c.Cmp(rc) != 0 {
		return false
	}
	totals := make([]*big.Rat, 4)
	for i, raw := range e.TotalCells {
		v, err := decimal(raw)
		if raw == "" && (i == 0 || i == 2) {
			continue
		}
		if err != nil {
			return false
		}
		totals[i] = v
	}
	for i, sum := range sums {
		if totals[i+1] != nil && totals[i+1].Cmp(sum) != 0 {
			return false
		}
	}
	if totals[2] == nil {
		return false
	} // Totals must independently validate notes.
	if e.GradeSource == "POINTS_TOTALS" {
		return true
	}
	if totals[0] == nil {
		return true
	}
	difference := new(big.Rat).Sub(new(big.Rat).Mul(totals[0], totals[1]), totals[2])
	difference.Abs(difference)
	return difference.Cmp(new(big.Rat).Mul(totals[1], big.NewRat(1, 100))) <= 0
}
