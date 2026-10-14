// Package totp implements RFC 6238 time-based one-time passwords (HMAC-SHA1, 30 s, 6 digits),
// the format every authenticator application understands, and renders the enrolment QR code.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

const (
	Period      = 30
	Digits      = 6
	SecretBytes = 20 // 160 bits, RFC 4226 recommendation
	// Skew accepts the previous and next time step for clock drift (±30 s).
	Skew = 1
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewSecret() ([]byte, error) {
	secret := make([]byte, SecretBytes)
	_, err := rand.Read(secret)
	return secret, err
}

// EncodeSecret is the base32 form typed into an authenticator when the QR code cannot be read.
func EncodeSecret(secret []byte) string { return encoding.EncodeToString(secret) }

func Step(t time.Time) int64 { return t.Unix() / Period }

// code computes HOTP (RFC 4226) for a counter with the requested number of digits.
func code(secret []byte, counter int64, digits int) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, secret)
	mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulo := uint32(1)
	for range digits {
		modulo *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulo)
}

// Code is the 6-digit code of a time step.
func Code(secret []byte, step int64) string { return code(secret, step, Digits) }

// Verify accepts a code of the current step ±Skew that is strictly newer than lastStep (the
// step of the last accepted code): a code is never accepted twice. It returns the matched step.
func Verify(secret []byte, input string, now time.Time, lastStep int64) (int64, bool) {
	input = strings.ReplaceAll(strings.TrimSpace(input), " ", "")
	if len(input) != Digits {
		return 0, false
	}
	current := Step(now)
	var matched int64
	found := 0
	for step := current - Skew; step <= current+Skew; step++ {
		// Every candidate is compared, in constant time, whatever the result.
		if subtle.ConstantTimeCompare([]byte(Code(secret, step)), []byte(input)) == 1 && step > lastStep && found == 0 {
			matched, found = step, 1
		}
	}
	return matched, found == 1
}

// URI is the otpauth:// key URI read by authenticator applications.
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{"secret": {EncodeSecret(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(Digits)}, "period": {fmt.Sprint(Period)}}
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// QRSVG renders text as a standalone SVG QR code (error correction M, 4-module quiet zone).
func QRSVG(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, size, size, size, size)
	for y := range c.Size {
		for x := range c.Size {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
