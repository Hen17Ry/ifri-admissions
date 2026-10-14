package analysis

import "testing"

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
			err := Validate(r, e, 0.85)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestCellArithmeticAcceptsLowOCRConfidenceOnlyWithCompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Extraction)
		valid  bool
	}{
		{"consistent low confidence", func(*Extraction) {}, true},
		{"unreadable points", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Points = "" }, true},
		{"wrong note correct scale", func(s *Extraction) {
			s.Grade = "18"
			s.GradeRaw = "18"
			s.Evidence.ArithmeticRows[0].Grade = "18"
			s.Evidence.ArithmeticRows[0].Points = ""
		}, false},
		{"derived with original divergence", func(s *Extraction) { s.GradeRaw = "18"; s.Evidence.GradeSource = "POINTS_TOTALS" }, true},
		{"derived wrong total", func(s *Extraction) {
			s.GradeRaw = "18"
			s.Evidence.GradeSource = "POINTS_TOTALS"
			s.Evidence.TotalCells[2] = "25"
		}, false},
		{"derived no row points", func(s *Extraction) {
			s.GradeRaw = "18"
			s.Evidence.GradeSource = "POINTS_TOTALS"
			s.Evidence.ArithmeticRows[0].Points = ""
		}, false},
		{"wrong row product", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Points = "30" }, false},
		{"wrong scale", func(s *Extraction) { s.Evidence.ArithmeticRows[0].Scale = "60" }, false},
		{"wrong totals", func(s *Extraction) { s.Evidence.TotalCells[2] = "30" }, false},
		{"wrong selected row", func(s *Extraction) { s.Evidence.ArithmeticRowIndex = 2 }, false},
		{"invalid series", func(s *Extraction) { s.Evidence.Series = "Z" }, false},
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
			b := s.Evidence.Grade
			s.Evidence.Points = &b
			s.Evidence.Scale = &b
			s.Evidence.ArithmeticRows = []ArithmeticRow{{Grade: "12", Coefficient: "2", Points: "24", Scale: "40"}}
			s.Evidence.TotalCells = []string{"12", "2", "24", "40"}
			tc.mutate(s)
			if err := Validate(r, e, .85); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
