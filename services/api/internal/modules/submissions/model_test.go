package submissions

import (
	"math/big"
	"testing"

	"ifri-admissions/services/api/internal/modules/analysis"
)

func grades(t *testing.T, pairs ...string) []Grade {
	t.Helper()
	out := []Grade{}
	for i := 0; i < len(pairs); i += 2 {
		value, err := ParseNumeric(pairs[i])
		if err != nil {
			t.Fatal(err)
		}
		coefficient, err := ParseNumeric(pairs[i+1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, Grade{value, coefficient})
	}
	return out
}

func TestComputeAverageExactDecimals(t *testing.T) {
	// 15.50 x 2.5 + 12.25 x 3 + 9 x 1 = 84.5 ; 84.5 / 6.5 = 13 exactly.
	a, err := ComputeAverage(grades(t, "15.50", "2.5", "12.25", "3", "9", "1"))
	if err != nil {
		t.Fatal(err)
	}
	weighted, _ := ExactDecimal(a.WeightedSum)
	coefficients, _ := ExactDecimal(a.CoefficientSum)
	if weighted != "84.5" || coefficients != "6.5" || a.Decimal != "13.000000000000000000000000000000" || !a.Exact {
		t.Fatalf("got %s / %s = %s exact=%v", weighted, coefficients, a.Decimal, a.Exact)
	}
	// Decimal inputs never pass through a float: 0.1 x 3 + 0.2 x 3 = 0.9 exactly.
	a, _ = ComputeAverage(grades(t, "0.1", "3", "0.2", "3"))
	if a.Decimal != "0.150000000000000000000000000000" || !a.Exact {
		t.Fatalf("float drift: %s", a.Decimal)
	}
}

func TestComputeAveragePeriodicValuesAreTruncatedNeverRounded(t *testing.T) {
	for _, tc := range []struct {
		name    string
		grades  []string
		decimal string
	}{
		{"thirds", []string{"10", "1", "10", "1", "11", "1"}, "10.333333333333333333333333333333"},
		{"two thirds", []string{"0", "1", "1", "1", "1", "1"}, "0.666666666666666666666666666666"},
		// 202 / 21 = 9.619047619047... (31st digit 6: rounding would end in 048).
		{"real transcript shape", []string{"10", "7", "9", "8", "10", "5", "10", "1"}, "9.619047619047619047619047619047"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := ComputeAverage(grades(t, tc.grades...))
			if err != nil {
				t.Fatal(err)
			}
			if a.Decimal != tc.decimal || a.Exact {
				t.Fatalf("got %s exact=%v", a.Decimal, a.Exact)
			}
			// The stored decimal is the truncation of the authoritative fraction.
			stored, _ := new(big.Rat).SetString(a.Decimal)
			if stored.Cmp(a.Fraction()) >= 0 || new(big.Rat).Sub(a.Fraction(), stored).Cmp(big.NewRat(1, 1)) >= 0 {
				t.Fatal("decimal is not a truncation")
			}
		})
	}
}

func TestEqualAtTwoDecimalsButDifferentInternally(t *testing.T) {
	periodic, _ := ComputeAverage(grades(t, "10", "1", "10", "1", "11", "1")) // 31/3
	exact, _ := ComputeAverage(grades(t, "10.33", "1"))
	left, _ := Truncate(periodic.Fraction(), 2)
	right, _ := Truncate(exact.Fraction(), 2)
	if left != "10.33" || right != "10.33" {
		t.Fatalf("display: %s %s", left, right)
	}
	if periodic.Decimal == exact.Decimal || periodic.Fraction().Cmp(exact.Fraction()) <= 0 {
		t.Fatal("internal precision lost")
	}
	// Even the 30-decimal column can tie (1/3 against 0.333...3 with 30 threes): the exactness
	// flag and the cross product of the stored fractions still separate them (ADR 086).
	third, _ := ComputeAverage(grades(t, "0", "1", "0", "1", "1", "1"))
	thirty, _ := ComputeAverage(grades(t, "0.333333333333333333333333333333", "1"))
	if third.Decimal != thirty.Decimal || third.Exact || !thirty.Exact {
		t.Fatalf("tie setup: %s %s", third.Decimal, thirty.Decimal)
	}
	cross := new(big.Rat).Mul(third.WeightedSum, thirty.CoefficientSum).Cmp(new(big.Rat).Mul(thirty.WeightedSum, third.CoefficientSum))
	if cross <= 0 {
		t.Fatal("cross product must rank 1/3 above 0.333...3")
	}
}

func TestComputeAverageRefusesIncompleteData(t *testing.T) {
	for name, input := range map[string][]Grade{
		"no subject":         nil,
		"zero coefficient":   grades(t, "12", "0"),
		"grade above twenty": grades(t, "20.01", "2"),
		"negative grade":     grades(t, "-1", "2"),
		"missing value":      {{nil, big.NewRat(1, 1)}},
	} {
		if _, err := ComputeAverage(input); err == nil {
			t.Fatal(name)
		}
	}
	for _, raw := range []string{"", "1e3", "1/3", "NaN", "abc"} {
		if _, err := ParseNumeric(raw); err == nil {
			t.Fatal("parsed", raw)
		}
	}
}

func TestExactDecimal(t *testing.T) {
	for in, want := range map[string]string{"84.5": "84.5", "21": "21", "0.125": "0.125", "40.75": "40.75", "0": "0"} {
		r, _ := new(big.Rat).SetString(in)
		if got, err := ExactDecimal(r); err != nil || got != want {
			t.Fatalf("%s: %s %v", in, got, err)
		}
	}
	if _, err := ExactDecimal(big.NewRat(1, 3)); err == nil {
		t.Fatal("1/3 has no finite decimal expansion")
	}
}

func TestFormatNumber(t *testing.T) {
	if n, err := FormatNumber(2026, "GL", 1); err != nil || n != "IFRI-2026-GL-000001" {
		t.Fatal(n, err)
	}
	if n, err := FormatNumber(2027, "SEIOT", 999999); err != nil || n != "IFRI-2027-SEIOT-999999" {
		t.Fatal(n, err)
	}
	for _, tc := range []struct {
		year    int
		program string
		serial  int
	}{{2026, "GL", 0}, {2026, "GL", 1000000}, {1999, "GL", 1}, {2026, "gl", 1}, {2026, "G-L", 1}} {
		if n, err := FormatNumber(tc.year, tc.program, tc.serial); err == nil {
			t.Fatal("accepted", n)
		}
	}
}

func TestMethod(t *testing.T) {
	cell := func(source string, rebuilt ...string) analysis.Evidence {
		return analysis.Evidence{Validation: "CELL_ARITHMETIC", GradeSource: source, ArithmeticRowIndex: 1,
			ArithmeticRows: []analysis.ArithmeticRow{{}, {Reconstructed: rebuilt}}}
	}
	for _, tc := range []struct {
		name     string
		evidence analysis.Evidence
		method   string
		cells    int
		valid    bool
	}{
		{"generic text or OCR path", analysis.Evidence{}, "DIRECT_READ", 0, true},
		{"note read and confirmed by its row", cell("DIRECT_CELL"), "ROW_ARITHMETIC", 0, true},
		{"note derived and validated by totals", cell("POINTS_TOTALS"), "TABLE_TOTALS", 0, true},
		{"reconstructed scale cell", cell("DIRECT_CELL", "scale"), "ROW_ARITHMETIC", 1, true},
		{"reconstructed points behind totals", cell("POINTS_TOTALS", "points"), "TABLE_TOTALS", 1, true},
		{"coefficient never reconstructed", cell("DIRECT_CELL", "coefficient"), "", 0, false},
		{"unknown grade source", cell("GUESS"), "", 0, false},
		{"row index outside table", analysis.Evidence{Validation: "CELL_ARITHMETIC", GradeSource: "DIRECT_CELL", ArithmeticRowIndex: 3}, "", 0, false},
	} {
		method, cells, err := Method(tc.evidence)
		if (err == nil) != tc.valid || method != tc.method || len(cells) != tc.cells {
			t.Fatalf("%s: %s %v %v", tc.name, method, cells, err)
		}
	}
}
