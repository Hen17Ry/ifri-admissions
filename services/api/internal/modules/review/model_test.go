package review

import (
	"strings"
	"testing"
)

func TestSafeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"relevé de notes.pdf":    "relevé de notes.pdf",
		"RELEVE.PDF":             "RELEVE.PDF",
		"../../etc/passwd":       "_.._etc_passwd.pdf",
		"a\"b\r\nc;d.pdf":        "a_b__c_d.pdf",
		"":                       "document.pdf",
		" . ":                    "document.pdf",
		strings.Repeat("é", 150): strings.Repeat("é", 100) + ".pdf",
	} {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// The list shows a truncated value; the exact fraction and its 30-place truncation stay available.
func TestAverageIsTruncatedNeverRounded(t *testing.T) {
	weighted, coefficients := "188", "15" // 12.5333…
	a := average(&weighted, &coefficients)
	if a.Display != "12.53" || a.Exact || a.Decimal != "12.533333333333333333333333333333" || a.WeightedSum != "188" || a.CoefficientSum != "15" {
		t.Fatalf("%+v", a)
	}
	weighted, coefficients = "199.99", "10" // 19.999: rounding would show 20.00
	if a = average(&weighted, &coefficients); a.Display != "19.99" || !a.Exact {
		t.Fatalf("%+v", a)
	}
	zero := "0"
	if average(&weighted, &zero) != nil || average(nil, &coefficients) != nil {
		t.Fatal("average without coefficients")
	}
}
