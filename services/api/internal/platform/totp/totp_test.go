package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 column (8 digits), key "12345678901234567890".
func TestRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:          "94287082",
		1111111109:  "07081804",
		1111111111:  "14050471",
		1234567890:  "89005924",
		2000000000:  "69279037",
		20000000000: "65353130",
	} {
		if got := code(secret, unix/Period, 8); got != want {
			t.Fatalf("T=%d: got %s, want %s", unix, got, want)
		}
		if got := Code(secret, unix/Period); got != want[2:] {
			t.Fatalf("6 digits T=%d: %s", unix, got)
		}
	}
}

func TestVerifySkewAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1234567890, 0)
	step := Step(now)
	for _, offset := range []int64{-1, 0, 1} {
		if matched, ok := Verify(secret, Code(secret, step+offset), now, 0); !ok || matched != step+offset {
			t.Fatalf("offset %d refused", offset)
		}
	}
	for _, offset := range []int64{-2, 2} {
		if _, ok := Verify(secret, Code(secret, step+offset), now, 0); ok {
			t.Fatalf("offset %d accepted", offset)
		}
	}
	// Anti-replay: the step already used, or an older one, is refused.
	if _, ok := Verify(secret, Code(secret, step), now, step); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok := Verify(secret, Code(secret, step-1), now, step); ok {
		t.Fatal("older code accepted after a newer one")
	}
	if matched, ok := Verify(secret, Code(secret, step+1), now, step); !ok || matched != step+1 {
		t.Fatal("next step refused after current")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := Verify(secret, bad, now, 0); ok {
			t.Fatal("accepted", bad)
		}
	}
	spaced := Code(secret, step)
	if _, ok := Verify(secret, spaced[:3]+" "+spaced[3:], now, 0); !ok {
		t.Fatal("spaced code refused")
	}
}

func TestSecretURIAndQR(t *testing.T) {
	secret, err := NewSecret()
	if err != nil || len(secret) != 20 || len(EncodeSecret(secret)) != 32 {
		t.Fatal("secret", err)
	}
	uri := URI("IFRI Admissions", "admin@ifri.example", secret)
	if !strings.HasPrefix(uri, "otpauth://totp/IFRI%20Admissions:admin@ifri.example?") || !strings.Contains(uri, "secret="+EncodeSecret(secret)) || !strings.Contains(uri, "period=30") {
		t.Fatal(uri)
	}
	svg, err := QRSVG(uri)
	if err != nil || !strings.HasPrefix(svg, "<svg xmlns=") || !strings.HasSuffix(svg, "</svg>") || strings.Contains(svg, "<script") {
		t.Fatal("svg", err)
	}
}
