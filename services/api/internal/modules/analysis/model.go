package analysis

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
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

// ArithmeticRow holds the cells of one table row as read ("" = unreadable). Grade is the note
// read directly; Reconstructed names the cells the worker derived from the Total row.
type ArithmeticRow struct {
	Grade         string   `json:"grade"`
	Coefficient   string   `json:"coefficient"`
	Points        string   `json:"points"`
	Scale         string   `json:"scale"`
	Reconstructed []string `json:"reconstructed,omitempty"`
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

var (
	decimalPattern   = regexp.MustCompile(`^[0-9]{1,64}([.,][0-9]{1,32})?$`)
	separatorSpacing = regexp.MustCompile(`\s+([.,])`)
)

// decimal parses exactly like the worker: one trailing separator after a number is dropped.
func decimal(raw string) (*big.Rat, error) {
	raw = strings.TrimSpace(raw)
	raw = separatorSpacing.ReplaceAllString(raw, "$1")
	if n := len(raw); n > 1 && (raw[n-1] == '.' || raw[n-1] == ',') && decimalPattern.MatchString(raw[:n-1]) {
		raw = raw[:n-1]
	}
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

// DeclaredSeries maps BAC_C to "C"; other diplomas have no BAC general series.
func DeclaredSeries(qualification string) string {
	if len(qualification) == 5 && strings.HasPrefix(qualification, "BAC_") {
		return qualification[4:]
	}
	return ""
}

// Validate re-checks a worker result independently. qualification is the declared origin
// qualification code of the analysed application ("" when unknown).
func Validate(result Result, expected []Expected, threshold float64, qualification string) error {
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
	failures := map[string]bool{"GRADE_POINTS_MISMATCH": true, "SERIES_MISMATCH": true, "COLUMN_SEGMENTATION_FAILED": true, "ROW_SEGMENTATION_FAILED": true, "NEEDS_REVIEW": true, "PDF_UNREADABLE": true, "NO_TEXT_FOUND": true, "SUBJECT_NOT_FOUND": true, "GRADE_NOT_FOUND": true, "COEFFICIENT_NOT_FOUND": true, "AMBIGUOUS_ROW": true, "LOW_CONFIDENCE": true, "OCR_ENGINE_ERROR": true, "ANALYSIS_TIMEOUT": true}
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
		// A derived note is only allowed when no note could be read in its cell.
		if s.Evidence.GradeSource == "POINTS_TOTALS" && err == nil && rg.Sign() >= 0 && rg.Cmp(twenty) <= 0 {
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
			if s.Evidence.Validation != "CELL_ARITHMETIC" || !validArithmetic(s, qualification) {
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
	return validRowUniqueness(result.Subjects)
}

// Two expected subjects never come from the same row, and cell evidence describes one table.
func validRowUniqueness(subjects []Extraction) error {
	type cell struct {
		page int
		box  BBox
	}
	grades := map[cell]bool{}
	rows := map[int]bool{}
	var table *Evidence
	for i := range subjects {
		s := subjects[i]
		key := cell{s.Page, s.Evidence.Grade}
		if grades[key] {
			return errors.New("row assignment duplicated")
		}
		grades[key] = true
		if s.Evidence.Validation != "CELL_ARITHMETIC" {
			continue
		}
		if rows[s.Evidence.ArithmeticRowIndex] {
			return errors.New("row assignment duplicated")
		}
		rows[s.Evidence.ArithmeticRowIndex] = true
		if table == nil {
			table = &subjects[i].Evidence
			continue
		}
		if !reflect.DeepEqual(table.ArithmeticRows, s.Evidence.ArithmeticRows) || !reflect.DeepEqual(table.TotalCells, s.Evidence.TotalCells) || table.Series != s.Evidence.Series {
			return errors.New("arithmetic tables inconsistent")
		}
	}
	return nil
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }

var twenty = big.NewRat(20, 1)

// cellRow is one evidence row parsed exactly; a nil cell is unreadable.
type cellRow struct {
	grade, coef, points, scale *big.Rat
	rebuilt                    map[string]bool
}

func (r cellRow) cell(name string) *big.Rat {
	switch name {
	case "coefficient":
		return r.coef
	case "points":
		return r.points
	case "scale":
		return r.scale
	}
	return nil
}

func optionalDecimal(raw string) (*big.Rat, bool) {
	if raw == "" {
		return nil, true
	}
	v, err := decimal(raw)
	return v, err == nil
}

// parseCellRows rejects malformed cells, impossible direct notes and any reconstruction that is
// not a single, non-empty cell of an additive column.
func parseCellRows(rows []ArithmeticRow) ([]cellRow, bool) {
	out := make([]cellRow, len(rows))
	perColumn := map[string]int{}
	for i, row := range rows {
		r := cellRow{rebuilt: map[string]bool{}}
		var ok [4]bool
		r.grade, ok[0] = optionalDecimal(row.Grade)
		r.coef, ok[1] = optionalDecimal(row.Coefficient)
		r.points, ok[2] = optionalDecimal(row.Points)
		r.scale, ok[3] = optionalDecimal(row.Scale)
		if !ok[0] || !ok[1] || !ok[2] || !ok[3] || (r.grade != nil && r.grade.Cmp(twenty) > 0) {
			return nil, false
		}
		for _, name := range row.Reconstructed {
			if r.cell(name) == nil || r.rebuilt[name] {
				return nil, false
			}
			r.rebuilt[name] = true
			perColumn[name]++
		}
		out[i] = r
	}
	for _, count := range perColumn {
		if count > 1 {
			return nil, false
		}
	}
	return out, true
}

// reconstructionsValid re-derives each reconstructed cell: Total minus the other rows.
func reconstructionsValid(rows []cellRow, totals []*big.Rat) bool {
	index := map[string]int{"coefficient": 1, "points": 2, "scale": 3}
	for i, r := range rows {
		for name := range r.rebuilt {
			total := totals[index[name]]
			if total == nil {
				return false
			}
			rest := new(big.Rat).Set(total)
			for j, other := range rows {
				if j == i {
					continue
				}
				if other.cell(name) == nil {
					return false
				}
				rest.Sub(rest, other.cell(name))
			}
			if rest.Cmp(r.cell(name)) != 0 {
				return false
			}
		}
	}
	return true
}

// coefficientValidated: integer 1..6 with Sur = 20 x Coeff, never two reconstructed operands.
func coefficientValidated(r cellRow) bool {
	if r.coef == nil || r.scale == nil || !r.coef.IsInt() || r.coef.Cmp(big.NewRat(1, 1)) < 0 || r.coef.Cmp(big.NewRat(6, 1)) > 0 {
		return false
	}
	return new(big.Rat).Mul(twenty, r.coef).Cmp(r.scale) == 0 && !(r.rebuilt["coefficient"] && r.rebuilt["scale"])
}

// pointsCheck: Points = Note x Coeff; known is false when a cell is missing.
func pointsCheck(r cellRow) (match, known bool) {
	if r.grade == nil || r.coef == nil || r.points == nil || (r.rebuilt["coefficient"] && r.rebuilt["points"]) {
		return false, false
	}
	return new(big.Rat).Mul(r.grade, r.coef).Cmp(r.points) == 0, true
}

// plausibleQuotient: Points / Coeff is a terminating decimal in [0, 20].
func plausibleQuotient(points, coef *big.Rat) bool {
	if points == nil || coef == nil || coef.Sign() <= 0 {
		return false
	}
	q := new(big.Rat).Quo(points, coef)
	if q.Sign() < 0 || q.Cmp(twenty) > 0 {
		return false
	}
	d := new(big.Int).Set(q.Denom())
	zero := new(big.Int)
	for _, f := range []int64{2, 5} {
		factor := big.NewInt(f)
		for new(big.Int).Mod(d, factor).Cmp(zero) == 0 {
			d.Quo(d, factor)
		}
	}
	return d.IsInt64() && d.Int64() == 1
}

// displayedAverageValid: the printed average is the truncation (observed on the Office du Bac
// transcript) or the half-up rounding to 2 decimals of Total Points / Total Coeff, nothing else.
func displayedAverageValid(displayed, coef, points *big.Rat) bool {
	scaled := new(big.Rat).Mul(new(big.Rat).Quo(points, coef), big.NewRat(100, 1))
	floor := func(v *big.Rat) *big.Rat {
		return new(big.Rat).SetFrac(new(big.Int).Quo(v.Num(), v.Denom()), big.NewInt(100))
	}
	return displayed.Cmp(floor(scaled)) == 0 || displayed.Cmp(floor(new(big.Rat).Add(scaled, big.NewRat(1, 2)))) == 0
}

// tableVerified mirrors the worker: the totals check every Points cell independently.
func tableVerified(rows []cellRow, totals []*big.Rat) bool {
	if len(rows) == 0 || totals[1] == nil || totals[2] == nil || totals[3] == nil {
		return false
	}
	sums := []*big.Rat{new(big.Rat), new(big.Rat), new(big.Rat)}
	for _, r := range rows {
		match, known := pointsCheck(r)
		if !coefficientValidated(r) || !plausibleQuotient(r.points, r.coef) || (known && !match) || (r.rebuilt["points"] && !known) {
			return false
		}
		sums[0].Add(sums[0], r.coef)
		sums[1].Add(sums[1], r.points)
		sums[2].Add(sums[2], r.scale)
	}
	for i, sum := range sums {
		if sum.Cmp(totals[i+1]) != 0 {
			return false
		}
	}
	return totals[0] == nil || displayedAverageValid(totals[0], totals[1], totals[2])
}

// Independent rational checks of one accepted BAC cell value (contract: worker
// ifri_ocr.bac.arithmetic). A note read on its row must equal Points / Coeff; a note derived
// from Points / Coeff requires an unreadable note cell, a Points cell read directly and a table
// verified by its totals in which every other row is confirmed by its read note. The
// coefficient is always its own cell. Low OCR confidence never
// bypasses these checks.
func validArithmetic(s Extraction, qualification string) bool {
	e := s.Evidence
	// An unreadable series is reported, not blocking; a read series must match the declaration.
	if e.Series != "" && (len(e.Series) != 1 || !strings.Contains("ABCDEFG", e.Series) || (qualification != "" && e.Series != DeclaredSeries(qualification))) {
		return false
	}
	if e.GradeSource != "DIRECT_CELL" && e.GradeSource != "POINTS_TOTALS" {
		return false
	}
	if e.Points == nil || e.Scale == nil || !validBox(*e.Points) || !validBox(*e.Scale) {
		return false
	}
	if e.Profile != "BENIN_BAC_GENERAL" || e.Section != "FIRST_GROUP_WRITTEN" || e.Preprocessing != "hsv-v" || e.PSM != 7 || len(e.ArithmeticRows) < 1 || len(e.ArithmeticRows) > 30 || len(e.TotalCells) != 4 || e.ArithmeticRowIndex < 0 || e.ArithmeticRowIndex >= len(e.ArithmeticRows) {
		return false
	}
	rows, ok := parseCellRows(e.ArithmeticRows)
	if !ok {
		return false
	}
	totals := make([]*big.Rat, 4)
	for i, raw := range e.TotalCells {
		if totals[i], ok = optionalDecimal(raw); !ok {
			return false
		}
	}
	if (totals[0] != nil && totals[0].Cmp(twenty) > 0) || !reconstructionsValid(rows, totals) {
		return false
	}
	selected := rows[e.ArithmeticRowIndex]
	g, eg := decimal(s.Grade)
	c, ec := decimal(s.Coefficient)
	if eg != nil || ec != nil || selected.coef == nil || c.Cmp(selected.coef) != 0 || selected.rebuilt["coefficient"] || !coefficientValidated(selected) {
		return false
	}
	if e.GradeSource == "DIRECT_CELL" {
		match, known := pointsCheck(selected)
		return selected.grade != nil && g.Cmp(selected.grade) == 0 && known && match
	}
	if selected.grade != nil || selected.points == nil || selected.rebuilt["points"] || !tableVerified(rows, totals) {
		return false
	}
	// The totals check this Points cell only if every other row is confirmed by its read note.
	for i, r := range rows {
		if match, known := pointsCheck(r); i != e.ArithmeticRowIndex && !(known && match) {
			return false
		}
	}
	return new(big.Rat).Mul(g, c).Cmp(selected.points) == 0
}
