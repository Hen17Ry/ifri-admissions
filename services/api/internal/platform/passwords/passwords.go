// Package passwords hashes administrator passwords with argon2id and applies the password
// policy: 12 to 128 characters, not a common password, not the account email. No composition
// rules (NIST SP 800-63B).
package passwords

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Params are the argon2id cost parameters, encoded in every PHC hash so they can evolve.
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltBytes int
	KeyBytes  int
}

// Default: 64 MiB, 3 passes, 2 lanes (RFC 9106 second recommendation adapted to 2 lanes).
var Default = Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 2, SaltBytes: 16, KeyBytes: 32}

const (
	MinRunes = 12
	MaxRunes = 128
	maxBytes = 512
)

var (
	ErrTooShort    = errors.New("PASSWORD_TOO_SHORT")
	ErrTooLong     = errors.New("PASSWORD_TOO_LONG")
	ErrTooCommon   = errors.New("PASSWORD_TOO_COMMON")
	ErrMatchesUser = errors.New("PASSWORD_MATCHES_ACCOUNT")
	errMalformed   = errors.New("password hash malformed")
)

//go:embed common-passwords.txt.gz
var commonArchive []byte

var common = sync.OnceValues(func() (map[string]struct{}, error) {
	reader, err := gzip.NewReader(bytes.NewReader(commonArchive))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	set := map[string]struct{}{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		set[scanner.Text()] = struct{}{}
	}
	return set, scanner.Err()
})

// CommonCount reports the embedded list size (startup check and tests).
func CommonCount() (int, error) {
	set, err := common()
	return len(set), err
}

// Normalize applies Unicode NFC so a password typed on another keyboard still matches.
func Normalize(password string) string { return norm.NFC.String(password) }

// Check enforces the policy on a normalized password.
func Check(password, email string) error {
	if !utf8.ValidString(password) || len(password) > maxBytes {
		return ErrTooLong
	}
	n := utf8.RuneCountInString(password)
	if n < MinRunes {
		return ErrTooShort
	}
	if n > MaxRunes {
		return ErrTooLong
	}
	lower := strings.ToLower(password)
	email = strings.ToLower(strings.TrimSpace(email))
	if local, _, found := strings.Cut(email, "@"); email != "" && (lower == email || (found && len(local) >= 4 && lower == local)) {
		return ErrMatchesUser
	}
	set, err := common()
	if err != nil {
		return err
	}
	if _, found := set[lower]; found {
		return ErrTooCommon
	}
	return nil
}

// Hasher bounds concurrent argon2 computations: each one holds MemoryKiB of RAM.
type Hasher struct {
	params Params
	slots  chan struct{}
	dummy  string
}

func NewHasher(params Params, concurrency int) (*Hasher, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	h := &Hasher{params: params, slots: make(chan struct{}, concurrency)}
	dummy, err := h.Hash(context.Background(), "dummy password for unknown accounts")
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Hash returns a PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$key.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, h.params.SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.slots }()
	key := argon2.IDKey([]byte(Normalize(password)), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, uint32(h.params.KeyBytes))
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify compares in constant time with the parameters stored in the hash.
func (h *Hasher) Verify(ctx context.Context, encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false, errMalformed
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil || p.MemoryKiB > 1<<20 || p.Time > 20 || p.Threads == 0 {
		return false, errMalformed
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errMalformed
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, errMalformed
	}
	if err = h.acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-h.slots }()
	got := argon2.IDKey([]byte(Normalize(password)), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// VerifyDummy spends the cost of a real verification for an unknown or unusable account, so
// the response time does not reveal whether the account exists.
func (h *Hasher) VerifyDummy(ctx context.Context, password string) error {
	_, err := h.Verify(ctx, h.dummy, password)
	return err
}

// NeedsRehash reports a hash made with other parameters (upgrade at next login).
func (h *Hasher) NeedsRehash(encoded string) bool {
	return !strings.Contains(encoded, fmt.Sprintf("$m=%d,t=%d,p=%d$", h.params.MemoryKiB, h.params.Time, h.params.Threads))
}
