// Package secrets derives purpose-bound subkeys from ADMIN_SECRETS_KEY with HKDF-SHA256.
// A subkey serves exactly one purpose: sealing TOTP secrets (AES-256-GCM), pseudonymising
// identifiers and IP addresses (HMAC-SHA256, rate limiting and audit), or deriving CSRF tokens.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// MinimumKeyBytes is the minimum decoded size of ADMIN_SECRETS_KEY.
const MinimumKeyBytes = 32

// ErrUnknownKeyVersion means a ciphertext was sealed by a key this process does not hold.
var ErrUnknownKeyVersion = errors.New("secret sealed with an unknown key version")

type Keys struct {
	version   int
	seal      cipher.AEAD
	pseudonym []byte
	csrf      []byte
}

// ParseMaster decodes a base64 (standard or URL, padded or not) master key of 32 bytes or more.
func ParseMaster(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := encoding.DecodeString(encoded); err == nil {
			if len(key) < MinimumKeyBytes {
				return nil, fmt.Errorf("ADMIN_SECRETS_KEY must decode to at least %d bytes", MinimumKeyBytes)
			}
			return key, nil
		}
	}
	return nil, errors.New("ADMIN_SECRETS_KEY must be base64")
}

// Derive builds the three subkeys of one master key version.
func Derive(master []byte, version int) (Keys, error) {
	if len(master) < MinimumKeyBytes || version < 1 || version > 32767 {
		return Keys{}, errors.New("admin secrets key invalid")
	}
	subkey := func(purpose string) ([]byte, error) {
		return hkdf.Key(sha256.New, master, nil, fmt.Sprintf("ifri-admissions/admin/%s/v%d", purpose, version), 32)
	}
	sealKey, err := subkey("totp-aes-256-gcm")
	if err != nil {
		return Keys{}, err
	}
	block, err := aes.NewCipher(sealKey)
	if err != nil {
		return Keys{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Keys{}, err
	}
	pseudonym, err := subkey("pseudonym-hmac-sha256")
	if err != nil {
		return Keys{}, err
	}
	csrf, err := subkey("csrf-hmac-sha256")
	if err != nil {
		return Keys{}, err
	}
	return Keys{version: version, seal: aead, pseudonym: pseudonym, csrf: csrf}, nil
}

func (k Keys) Version() int { return k.version }

// Seal encrypts plaintext bound to aad (the owner's identifier) and returns the key version.
func (k Keys) Seal(plaintext, aad []byte) (version int, nonce, ciphertext []byte, err error) {
	nonce = make([]byte, k.seal.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return 0, nil, nil, err
	}
	return k.version, nonce, k.seal.Seal(nil, nonce, plaintext, aad), nil
}

// Open decrypts a value sealed by Seal; a different key version is refused, never guessed.
func (k Keys) Open(version int, nonce, ciphertext, aad []byte) ([]byte, error) {
	if version != k.version {
		return nil, ErrUnknownKeyVersion
	}
	return k.seal.Open(nil, nonce, ciphertext, aad)
}

// Pseudonym is a keyed, non-reversible identifier for a value within a domain ("email", "ip").
func (k Keys) Pseudonym(domain, value string) string {
	mac := hmac.New(sha256.New, k.pseudonym)
	mac.Write([]byte(domain))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// CSRF derives the synchronizer token of a session from its token hash: nothing extra is
// stored, and the token changes whenever the session token rotates.
func (k Keys) CSRF(sessionTokenHash []byte) string {
	mac := hmac.New(sha256.New, k.csrf)
	mac.Write(sessionTokenHash)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidCSRF compares in constant time.
func (k Keys) ValidCSRF(sessionTokenHash []byte, token string) bool {
	return hmac.Equal([]byte(k.CSRF(sessionTokenHash)), []byte(token))
}
