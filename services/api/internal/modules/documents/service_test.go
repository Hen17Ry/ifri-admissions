package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/modules/references"
	"ifri-admissions/services/api/internal/platform/storage"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

type inspector struct{ err error }

func (i inspector) Check(context.Context, string) error { return i.err }

type memoryRepo struct {
	d            drafts.Draft
	rows         []Document
	err, saveErr error
	saved        Version
}

func (r *memoryRepo) Draft(context.Context, []byte) (drafts.Draft, error)      { return r.d, r.err }
func (r *memoryRepo) List(context.Context, string) ([]Document, error)         { return r.rows, nil }
func (r *memoryRepo) Current(context.Context, string, string) (Version, error) { return r.saved, nil }
func (r *memoryRepo) Save(_ context.Context, _ []byte, _ int64, code string, v Version) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = v
	r.d.Version++
	r.rows = append(r.rows, Document{Type: Type{Code: code}, Uploaded: true, Version: 1, Filename: v.Filename, Size: v.Size})
	return nil
}

type memoryObject struct{ *bytes.Reader }

func (memoryObject) Close() error { return nil }

type memoryStorage struct {
	data    map[string][]byte
	err     error
	deleted []string
}

func (s *memoryStorage) Put(_ context.Context, k string, r io.Reader, _ int64) error {
	if s.err != nil {
		return s.err
	}
	data, _ := io.ReadAll(r)
	s.data[k] = data
	return nil
}
func (s *memoryStorage) Get(_ context.Context, k string) (storage.Object, error) {
	v, ok := s.data[k]
	if !ok {
		return storage.Object{}, errors.New("missing")
	}
	return storage.Object{Body: memoryObject{bytes.NewReader(v)}, Size: int64(len(v)), ContentType: "application/pdf"}, nil
}
func (s *memoryStorage) Delete(_ context.Context, k string) error {
	s.deleted = append(s.deleted, k)
	delete(s.data, k)
	return nil
}
func fixture() (*Service, *memoryRepo, *memoryStorage) {
	repo := &memoryRepo{d: drafts.Draft{ID: "11111111-1111-4111-8111-111111111111", State: "DRAFT", Version: 1, Campaign: references.Campaign{Status: "OPEN", StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour)}}}
	objects := &memoryStorage{data: map[string][]byte{}}
	return NewService(repo, objects, inspector{}, 10485760, slog.New(slog.NewTextHandler(io.Discard, nil))), repo, objects
}
func TestValidation(t *testing.T) {
	valid, _ := os.ReadFile("testdata/valid.pdf")
	for _, tc := range []struct {
		name, filename, mime string
		data                 []byte
		max                  int64
		bad                  bool
	}{
		{"valid", "file.pdf", "application/pdf", valid, 1024, true},
		{"valid larger limit", "file.pdf", "application/pdf", valid, 10485760, false},
		{"fake", "file.pdf", "application/pdf", []byte("MZ image renamed"), 10485760, true},
		{"empty", "file.pdf", "application/pdf", nil, 10485760, true},
		{"too large", "file.pdf", "application/pdf", valid, 10, true},
		{"extension", "file.exe", "application/pdf", valid, 10485760, true},
		{"mime", "file.pdf", "image/png", valid, 10485760, true},
	} {
		if tc.name == "valid" {
			tc.bad = int64(len(valid)) > tc.max
		}
		t.Run(tc.name, func(t *testing.T) {
			u, err := Validate(context.Background(), bytes.NewReader(tc.data), tc.filename, tc.mime, tc.max, inspector{})
			if (err != nil) != tc.bad {
				t.Fatalf("validation: %v", err)
			}
			if err == nil {
				defer u.Close()
				hash := sha256.Sum256(tc.data)
				if u.Hash != hex.EncodeToString(hash[:]) {
					t.Fatal("checksum mismatch")
				}
			}
		})
	}
	u, err := Validate(context.Background(), bytes.NewReader(valid), "../../private\\evil\r\n.pdf", "application/pdf", 10485760, inspector{})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if strings.ContainsAny(u.Filename, "/\\\r\n") {
		t.Fatal("unsafe filename")
	}
}
func TestUploadAndCleanup(t *testing.T) {
	data, _ := os.ReadFile("testdata/valid.pdf")
	for _, tc := range []struct {
		name    string
		db, put error
		deleted bool
	}{
		{"ok", nil, nil, false}, {"DB failure", errors.New("DB"), nil, true}, {"storage failure", nil, errors.New("storage"), true}, {"uncertain commit", ErrCommitUnknown, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, obj := fixture()
			r.saveErr = tc.db
			obj.err = tc.put
			_, err := s.Upload(context.Background(), nil, 1, "TRANSCRIPT", "../../original.pdf", "application/pdf", bytes.NewReader(data))
			if tc.db == nil && tc.put == nil && err != nil {
				t.Fatal(err)
			}
			if tc.deleted != (len(obj.deleted) > 0) {
				t.Fatal("cleanup policy")
			}
			if r.saved.Key != "" {
				if strings.Contains(r.saved.Key, "original") || strings.Contains(r.saved.Key, "..") || r.saved.SHA256 == "" {
					t.Fatal("unsafe generated key or checksum")
				}
			}
		})
	}
}
func TestAuthorizationAndCompleteness(t *testing.T) {
	s, r, _ := fixture()
	out, err := s.List(context.Background(), nil)
	if err != nil || out.Complete || len(out.Documents) != 4 {
		t.Fatal("incomplete list")
	}
	for _, ty := range Types {
		r.rows = append(r.rows, Document{Type: ty, Uploaded: true, Version: 1})
	}
	out, err = s.List(context.Background(), nil)
	if err != nil || !out.Complete {
		t.Fatal("complete list")
	}
	for _, e := range []error{drafts.ErrSession, drafts.ErrExpired} {
		r.err = e
		if _, err = s.List(context.Background(), nil); !errors.Is(err, e) {
			t.Fatal("session check missing")
		}
	}
	r.err = nil
	r.d.State = "SUBMITTED"
	// Locked before the optimistic version: a stale version still gets APPLICATION_LOCKED.
	if _, err = s.Upload(context.Background(), nil, 2, "TRANSCRIPT", "x.pdf", "application/pdf", nil); !errors.Is(err, drafts.ErrLocked) {
		t.Fatal("non-draft upload", err)
	}
	if _, err = s.List(context.Background(), nil); !errors.Is(err, drafts.ErrLocked) {
		t.Fatal("submitted documents still listed", err)
	}
	if _, _, err = s.Content(context.Background(), nil, "TRANSCRIPT"); !errors.Is(err, drafts.ErrLocked) {
		t.Fatal("submitted document still served", err)
	}
	r.d.State = "DRAFT"
	if _, err = s.Upload(context.Background(), nil, 1, "UNKNOWN", "x.pdf", "application/pdf", nil); err == nil {
		t.Fatal("unknown type")
	}
	if _, err = s.Upload(context.Background(), nil, 2, "TRANSCRIPT", "x.pdf", "application/pdf", nil); !errors.Is(err, drafts.ErrConflict) {
		t.Fatal("stale version")
	}
}
