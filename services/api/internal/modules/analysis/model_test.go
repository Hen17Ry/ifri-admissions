package analysis

import (
	"math/big"
	"testing"
)

func validResult() (Result, []Expected) {
	b := BBox{0.1, 0.1, 0.2, 0.2}
	s := Extraction{SubjectID: "subject", Position: 1, Detected: "Mathématiques", GradeRaw: "15,50", Grade: "15.50", CoefficientRaw: "2,5", Coefficient: "2.5", Page: 1, SubjectConfidence: 1, GradeConfidence: 1, CoefficientConfidence: 1, Confidence: 1, Evidence: Evidence{Subject: b, Grade: b, Coefficient: b, System: "NORMALIZED_TOP_LEFT"}}
	return Result{ContractVersion: 1, Status: "SUCCEEDED", Engine: "native", EngineVersion: "1", Subjects: []Extraction{s}}, []Expected{{ID: "subject", Position: 1}}
}
func TestValidate(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Result)
		valid  bool
	}{
		{"valid", func(*Result) {}, true},
		{"scan metadata", func(r *Result) {
			r.Subjects[0].Evidence.Profile = "BENIN_BAC_GENERAL"
			r.Subjects[0].Evidence.Section = "FIRST_GROUP_WRITTEN"
			r.Subjects[0].Evidence.Preprocessing = "adaptive"
			r.Subjects[0].Evidence.DPI = 300
			r.Subjects[0].Evidence.PSM = 6
		}, true},
		{"unsafe scan profile", func(r *Result) { r.Subjects[0].Evidence.Profile = "arbitrary" }, false},
		{"unbounded scan dpi", func(r *Result) { r.Subjects[0].Evidence.DPI = 1000 }, false},
		{"grade above scale", func(r *Result) { r.Subjects[0].Grade = "21" }, false},
		{"coefficient zero", func(r *Result) { r.Subjects[0].Coefficient = "0" }, false},
		{"coefficient inferred", func(r *Result) { r.Subjects[0].CoefficientRaw = "" }, false},
		{"unexpected subject", func(r *Result) { r.Subjects[0].SubjectID = "other" }, false},
		{"missing subject", func(r *Result) { r.Subjects = nil }, false},
		{"ambiguous confidence", func(r *Result) { r.Subjects[0].Confidence = 0.2 }, false},
		{"bbox invalid", func(r *Result) { r.Subjects[0].Evidence.Grade.X1 = 2 }, false},
		{"low confidence terminal", func(r *Result) { r.Status = "LOW_CONFIDENCE"; r.Failure = "AMBIGUOUS_ROW"; r.Subjects = nil }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, e := validResult()
			test.mutate(&r)
			err := Validate(r, e, 0.85, "")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestCellArithmeticAcceptsLowOCRConfidenceOnlyWithCompleteEvidence(t *testing.T) {
	derived := func(s *Extraction) {
		s.GradeRaw = ""
		s.Evidence.GradeSource = "POINTS_TOTALS"
		s.Evidence.ArithmeticRows[0].Grade = ""
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Extraction)
		valid  bool
	}{
		{"consistent low confidence", func(*Extraction) {}, true},
		{"direct note ignores a wrong total", func(s *Extraction) { s.Evidence.TotalCells[2] = "30" }, true},
		{"unreadable points", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Points = "" }, false},
		{"points reconstructed from the total", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Reconstructed = []string{"points"} }, true},
		{"reconstruction differs from the total", func(s *Extraction) {
			s.Evidence.ArithmeticRows[0].Reconstructed = []string{"points"}
			s.Evidence.TotalCells[2] = "26"
		}, false},
		{"unknown reconstructed cell", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Reconstructed = []string{"grade"} }, false},
		{"returned coefficient reconstructed", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Reconstructed = []string{"coefficient"} }, false},
		{"wrong note correct scale", func(s *Extraction) {
			s.Grade = "18"
			s.GradeRaw = "18"
			s.Evidence.ArithmeticRows[0].Grade = "18"
			s.Evidence.ArithmeticRows[0].Points = ""
		}, false},
		{"derived from an unreadable note", derived, true},
		{"derived despite a readable note", func(s *Extraction) { derived(s); s.GradeRaw = "18" }, false},
		{"derived wrong total", func(s *Extraction) { derived(s); s.Evidence.TotalCells[2] = "25" }, false},
		{"derived no row points", func(s *Extraction) { derived(s); s.Evidence.ArithmeticRows[0].Points = "" }, false},
		{"derived from reconstructed points", func(s *Extraction) {
			derived(s)
			s.Evidence.ArithmeticRows[0].Reconstructed = []string{"points"}
		}, false},
		{"derived wrong displayed average", func(s *Extraction) { derived(s); s.Evidence.TotalCells[0] = "11.90" }, false},
		{"wrong row product", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Points = "30" }, false},
		{"wrong scale", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Scale = "60" }, false},
		{"missing grade source", func(s *Extraction) { s.Evidence.GradeSource = "" }, false},
		{"wrong selected row", func(s *Extraction) { s.Evidence.ArithmeticRowIndex = 2 }, false},
		{"invalid series", func(s *Extraction) { s.Evidence.Series = "Z" }, false},
		{"unreadable series is not blocking", func(s *Extraction) { s.Evidence.Series = "" }, true},
		{"series differs from declaration", func(s *Extraction) { s.Evidence.Series = "D" }, false},
		{"no arithmetic validation", func(s *Extraction) { s.Evidence.Validation = "" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, e := validResult()
			s := &r.Subjects[0]
			s.GradeRaw = "12"
			s.Grade = "12"
			s.CoefficientRaw = "2"
			s.Coefficient = "2"
			s.SubjectConfidence = .2
			s.GradeConfidence = .3
			s.CoefficientConfidence = .4
			s.Confidence = .2
			s.Evidence.Profile = "BENIN_BAC_GENERAL"
			s.Evidence.Section = "FIRST_GROUP_WRITTEN"
			s.Evidence.Preprocessing = "hsv-v"
			s.Evidence.PSM = 7
			s.Evidence.DPI = 300
			s.Evidence.Series = "C"
			s.Evidence.Validation = "CELL_ARITHMETIC"
			s.Evidence.GradeSource = "DIRECT_CELL"
			b := s.Evidence.Grade
			s.Evidence.Points = &b
			s.Evidence.Scale = &b
			s.Evidence.ArithmeticRows = []ArithmeticRow{{Grade: "12", Coefficient: "2", Points: "24", Scale: "40"}}
			s.Evidence.TotalCells = []string{"12", "2", "24", "40"}
			tc.mutate(s)
			if err := Validate(r, e, .85, "BAC_C"); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

// Four rows whose Total is 21 / 202 / 420: average 202/21 = 9.619..., printed truncated (9.61).
func tableResult(selected int, rows []ArithmeticRow, total []string) (Result, []Expected) {
	r, e := validResult()
	s := &r.Subjects[0]
	s.GradeRaw, s.Grade = rows[selected].Grade, rows[selected].Grade
	s.CoefficientRaw, s.Coefficient = rows[selected].Coefficient, rows[selected].Coefficient
	s.Evidence.Profile, s.Evidence.Section = "BENIN_BAC_GENERAL", "FIRST_GROUP_WRITTEN"
	s.Evidence.Preprocessing, s.Evidence.PSM, s.Evidence.DPI = "hsv-v", 7, 300
	s.Evidence.Validation, s.Evidence.GradeSource = "CELL_ARITHMETIC", "DIRECT_CELL"
	b := s.Evidence.Grade
	s.Evidence.Points, s.Evidence.Scale = &b, &b
	s.Evidence.ArithmeticRows, s.Evidence.ArithmeticRowIndex, s.Evidence.TotalCells = rows, selected, total
	return r, e
}

func fourRows() []ArithmeticRow {
	return []ArithmeticRow{{"10", "5", "50", "100", nil}, {"9", "6", "54", "120", nil}, {"8", "4", "32", "80", nil}, {"11", "6", "66", "120", nil}}
}

func TestDerivedNoteNeedsAnIndependentlyVerifiedTable(t *testing.T) {
	deriveRow1 := func(rows []ArithmeticRow, total []string) error {
		rows[1].Grade = ""
		r, e := tableResult(1, rows, total)
		points, _ := decimal(rows[1].Points)
		grade := new(big.Rat).Quo(points, big.NewRat(6, 1)).FloatString(2)
		r.Subjects[0].GradeRaw, r.Subjects[0].Grade, r.Subjects[0].Evidence.GradeSource = "", grade, "POINTS_TOTALS"
		return Validate(r, e, .85, "")
	}
	for _, tc := range []struct {
		name   string
		mutate func([]ArithmeticRow, []string)
		valid  bool
	}{
		{"truncated average", func([]ArithmeticRow, []string) {}, true},
		{"rounded average", func(_ []ArithmeticRow, total []string) { total[0] = "9.62" }, true},
		{"other average", func(_ []ArithmeticRow, total []string) { total[0] = "9.60" }, false},
		{"unreadable average", func(_ []ArithmeticRow, total []string) { total[0] = "" }, true},
		{"impossible average sent as evidence", func(_ []ArithmeticRow, total []string) { total[0] = "961" }, false},
		{"confirmed reconstruction elsewhere", func(rows []ArithmeticRow, _ []string) { rows[2].Reconstructed = []string{"scale"} }, true},
		{"two reconstructions in a column", func(rows []ArithmeticRow, _ []string) {
			rows[2].Reconstructed = []string{"scale"}
			rows[3].Reconstructed = []string{"scale"}
		}, false},
		{"other row coefficient invalid", func(rows []ArithmeticRow, _ []string) { rows[3].Coefficient = "7" }, false},
		{"compensation between two unreadable notes", func(rows []ArithmeticRow, _ []string) {
			rows[2].Grade = ""
			rows[1].Points, rows[2].Points = "60", "26" // 54+6, 32-6: sums unchanged, 10 and 6.5.
		}, false},
		{"compensation revealed by a readable note", func(rows []ArithmeticRow, _ []string) {
			rows[1].Points, rows[2].Points = "60", "26" // Row 2 still reads 8: 8 x 4 != 26.
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := fourRows(), []string{"9.61", "21", "202", "420"}
			tc.mutate(rows, total)
			if err := deriveRow1(rows, total); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestCompensatedPointsNeverValidateADirectNote(t *testing.T) {
	rows := fourRows()
	rows[0].Points, rows[1].Points = "52", "52" // 50+2, 54-2: sums still equal the Total.
	for _, selected := range []int{0, 1} {
		r, e := tableResult(selected, rows, []string{"9.61", "21", "202", "420"})
		if err := Validate(r, e, .85, ""); err == nil {
			t.Fatal("compensated Points accepted on row", selected)
		}
	}
	r, e := tableResult(2, rows, []string{"9.61", "21", "202", "420"})
	if err := Validate(r, e, .85, ""); err != nil {
		t.Fatal("untouched row refused", err)
	}
}

func TestSeriesAgainstDeclaredQualification(t *testing.T) {
	if DeclaredSeries("BAC_D") != "D" || DeclaredSeries("DT_IMI") != "" || DeclaredSeries("") != "" {
		t.Fatal("declared series mapping")
	}
	r, e := cellResult()
	if err := Validate(r, e, .85, "DT_IMI"); err == nil {
		t.Fatal("BAC general series accepted for a DT declaration")
	}
	if err := Validate(r, e, .85, ""); err != nil {
		t.Fatal("unknown declaration must not block", err)
	}
	r.Status, r.Failure, r.Subjects = "FAILED", "SERIES_MISMATCH", nil
	if err := Validate(r, e, .85, "BAC_D"); err != nil {
		t.Fatal("SERIES_MISMATCH terminal result refused", err)
	}
}

func TestTwoSubjectsNeverShareOneRow(t *testing.T) {
	r, e := cellResult()
	second := r.Subjects[0]
	second.SubjectID, second.Position = "other", 2
	second.Evidence.Grade = BBox{0.3, 0.3, 0.4, 0.4}
	second.Evidence.ArithmeticRows = append([]ArithmeticRow{}, r.Subjects[0].Evidence.ArithmeticRows...)
	r.Subjects = append(r.Subjects, second)
	e = append(e, Expected{ID: "other", Position: 2})
	if err := Validate(r, e, .85, "BAC_C"); err == nil {
		t.Fatal("same arithmetic row accepted for two subjects")
	}
	r.Subjects[1].Evidence.ArithmeticRowIndex = 1
	if err := Validate(r, e, .85, "BAC_C"); err != nil {
		t.Fatal("distinct rows refused", err)
	}
	r.Subjects[1].Evidence.TotalCells = []string{"12", "4", "48", "81"}
	if err := Validate(r, e, .85, "BAC_C"); err == nil {
		t.Fatal("inconsistent table copies accepted")
	}
	r.Subjects[1].Evidence.TotalCells = r.Subjects[0].Evidence.TotalCells
	r.Subjects[1].Evidence.Grade = r.Subjects[0].Evidence.Grade
	if err := Validate(r, e, .85, "BAC_C"); err == nil {
		t.Fatal("same grade cell accepted for two subjects")
	}
}

// Two-row BAC table (12x2, 12x2) with Total 12/4/48/80 on series C.
func cellResult() (Result, []Expected) {
	r, e := validResult()
	s := &r.Subjects[0]
	s.GradeRaw, s.Grade, s.CoefficientRaw, s.Coefficient = "12", "12", "2", "2"
	s.Evidence.Profile = "BENIN_BAC_GENERAL"
	s.Evidence.Section = "FIRST_GROUP_WRITTEN"
	s.Evidence.Preprocessing = "hsv-v"
	s.Evidence.PSM = 7
	s.Evidence.DPI = 300
	s.Evidence.Series = "C"
	s.Evidence.Validation = "CELL_ARITHMETIC"
	s.Evidence.GradeSource = "DIRECT_CELL"
	b := s.Evidence.Grade
	s.Evidence.Points = &b
	s.Evidence.Scale = &b
	s.Evidence.ArithmeticRows = []ArithmeticRow{{Grade: "12", Coefficient: "2", Points: "24", Scale: "40"}, {Grade: "12", Coefficient: "2", Points: "24", Scale: "40"}}
	s.Evidence.TotalCells = []string{"12", "4", "48", "80"}
	return r, e
}
