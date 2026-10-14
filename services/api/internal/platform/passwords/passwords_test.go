package passwords

import (
	"context"
	"strings"
	"testing"
)

// Cheap parameters for unit tests only; production uses Default.
var fast = Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltBytes: 16, KeyBytes: 32}

func TestHashAndVerify(t *testing.T) {
	h, err := NewHasher(fast, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hash, err := h.Hash(ctx, "correct horse battery staple")
	if err != nil || !strings.HasPrefix(hash, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatal(hash, err)
	}
	other, _ := h.Hash(ctx, "correct horse battery staple")
	if other == hash {
		t.Fatal("salt not random")
	}
	if ok, err := h.Verify(ctx, hash, "correct horse battery staple"); !ok || err != nil {
		t.Fatal("valid password refused", err)
	}
	if ok, _ := h.Verify(ctx, hash, "correct horse battery stapler"); ok {
		t.Fatal("wrong password accepted")
	}
	// NFC: "é" precomposed and decomposed are the same password.
	composed, _ := h.Hash(ctx, "mot de passe déjà vu")
	if ok, _ := h.Verify(ctx, composed, "mot de passe déjà vu"); !ok {
		t.Fatal("normalization missing")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=64,t=1,p=1$AA$AA", "$argon2id$v=19$m=99999999,t=1,p=1$AA$AA", "$argon2id$v=19$m=64,t=1,p=1$!!$AA"} {
		if _, err := h.Verify(ctx, bad, "x"); err == nil {
			t.Fatal("malformed hash accepted", bad)
		}
	}
	if err := h.VerifyDummy(ctx, "anything"); err != nil {
		t.Fatal(err)
	}
	if h.NeedsRehash(hash) || !New(t).NeedsRehash(hash) {
		t.Fatal("rehash detection")
	}
}

func New(t *testing.T) *Hasher {
	t.Helper()
	h, err := NewHasher(Params{MemoryKiB: 128, Time: 1, Threads: 1, SaltBytes: 16, KeyBytes: 32}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestDefaultParameters(t *testing.T) {
	if Default.MemoryKiB != 65536 || Default.Time != 3 || Default.Threads != 2 {
		t.Fatal("documented argon2id parameters changed")
	}
}

func TestPolicy(t *testing.T) {
	if n, err := CommonCount(); err != nil || n < 40000 {
		t.Fatal("common password list missing", n, err)
	}
	for _, tc := range []struct {
		password, email string
		want            error
	}{
		{"elevenchars", "", ErrTooShort},
		{"twelve chars", "", nil},
		{strings.Repeat("é", 12), "", nil},
		{strings.Repeat("a", 129), "", ErrTooLong},
		{"123456789012", "", ErrTooCommon},
		{"QWERTYUIOPASDFGH", "", ErrTooCommon},
		{"Password1234", "", ErrTooCommon},
		{"Admin@Example.org", "admin@example.org", ErrMatchesUser},
		{"responsable.gl", "responsable.gl@ifri.example", ErrMatchesUser},
		{"une phrase de passe plutôt longue", "a@b.c", nil},
		{"aaaaaaaaaaaaaa1", "", nil},
	} {
		if err := Check(Normalize(tc.password), tc.email); err != tc.want {
			t.Fatalf("%q: got %v, want %v", tc.password, err, tc.want)
		}
	}
}
