package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testKeys(t *testing.T, fill byte, version int) Keys {
	t.Helper()
	k, err := Derive(bytes.Repeat([]byte{fill}, 32), version)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSubkeysAreSeparated(t *testing.T) {
	k := testKeys(t, 7, 1)
	if bytes.Equal(k.pseudonym, k.csrf) {
		t.Fatal("one subkey used for two purposes")
	}
	other := testKeys(t, 7, 2)
	if bytes.Equal(k.pseudonym, other.pseudonym) || k.Pseudonym("email", "a@b.c") == other.Pseudonym("email", "a@b.c") {
		t.Fatal("key versions share a subkey")
	}
	if k.Pseudonym("email", "a@b.c") == k.Pseudonym("ip", "a@b.c") {
		t.Fatal("pseudonym domains collide")
	}
	if len(k.Pseudonym("email", "a@b.c")) != 64 {
		t.Fatal("pseudonym is not a hex SHA-256 HMAC")
	}
}

func TestSealBindsOwnerAndVersion(t *testing.T) {
	k := testKeys(t, 3, 1)
	version, nonce, sealed, err := k.Seal([]byte("totp secret"), []byte("admin:1"))
	if err != nil || version != 1 || len(nonce) != 12 {
		t.Fatal(version, err)
	}
	if bytes.Contains(sealed, []byte("totp secret")) {
		t.Fatal("plaintext visible")
	}
	if plain, err := k.Open(1, nonce, sealed, []byte("admin:1")); err != nil || string(plain) != "totp secret" {
		t.Fatal("open", err)
	}
	if _, err := k.Open(1, nonce, sealed, []byte("admin:2")); err == nil {
		t.Fatal("secret moved to another account")
	}
	if _, err := k.Open(2, nonce, sealed, []byte("admin:1")); err != ErrUnknownKeyVersion {
		t.Fatal("unknown key version accepted", err)
	}
	if _, err := testKeys(t, 4, 1).Open(1, nonce, sealed, []byte("admin:1")); err == nil {
		t.Fatal("another master key opened the secret")
	}
}

func TestCSRFFollowsTheSessionToken(t *testing.T) {
	k := testKeys(t, 9, 1)
	token := k.CSRF([]byte("session-a"))
	if !k.ValidCSRF([]byte("session-a"), token) || k.ValidCSRF([]byte("session-b"), token) || k.ValidCSRF([]byte("session-a"), "") {
		t.Fatal("CSRF token not bound to its session")
	}
}

func TestParseMaster(t *testing.T) {
	raw := bytes.Repeat([]byte{1}, 32)
	for _, encoded := range []string{base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)} {
		if key, err := ParseMaster(encoded); err != nil || !bytes.Equal(key, raw) {
			t.Fatal(encoded, err)
		}
	}
	for _, bad := range []string{"", "not base64 !", base64.StdEncoding.EncodeToString(raw[:31])} {
		if _, err := ParseMaster(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if _, err := Derive(raw, 0); err == nil {
		t.Fatal("version 0 accepted")
	}
}
