//go:build integration

package documents

import (
	"context"
	"ifri-admissions/services/api/internal/platform/pdfcheck"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealQPDF(t *testing.T) {
	for _, file := range []string{"valid.pdf", "fake.pdf", "empty.pdf", "corrupt.pdf"} {
		t.Run(file, func(t *testing.T) {
			err := (pdfcheck.QPDF{}).Check(context.Background(), filepath.Join("testdata", file))
			if (err == nil) != (file == "valid.pdf") {
				t.Fatalf("qpdf validation: %v", err)
			}
		})
	}
	if err := (pdfcheck.QPDF{}).Check(context.Background(), filepath.Join("testdata", "active.pdf")); err != pdfcheck.ErrActive {
		t.Fatalf("active PDF accepted: %v", err)
	}

	encrypted := filepath.Join(t.TempDir(), "encrypted.pdf")
	if err := exec.Command("qpdf", "--encrypt", "test", "owner", "256", "--", "testdata/valid.pdf", encrypted).Run(); err != nil {
		t.Fatal(err)
	}
	if err := (pdfcheck.QPDF{}).Check(context.Background(), encrypted); err != pdfcheck.ErrEncrypted {
		t.Fatal("encrypted accepted")
	}
}
