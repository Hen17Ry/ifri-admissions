//go:build integration

package documents

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/drafts"
	"ifri-admissions/services/api/internal/platform/pdfcheck"
	"ifri-admissions/services/api/internal/platform/storage"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestPostgresDocuments(t *testing.T) {
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	schema := "ifri_document_test_" + hex.EncodeToString(token)
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("database unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("cleanup failed")
		}
	})
	pc, err := pgxpool.ParseConfig(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := stdlib.OpenDB(*pc.ConnConfig)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	// Test incremental upgrade from phase 3, rather than only empty -> latest.
	if _, err = provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err = seeds.Development(ctx, pool, "test"); err != nil {
		t.Fatal(err)
	}
	_, hash, _ := drafts.NewSecret()
	draft, err := drafts.NewRepository(pool).Create(ctx, "DEV_LOCAL", hash, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := storage.NewS3(c.S3Endpoint, c.S3Bucket, c.S3Region, c.S3AccessKey, c.S3SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(pool), objects, pdfcheck.QPDF{}, c.DocumentMaxBytes, slog.New(slog.NewTextHandler(io.Discard, nil)))
	data, err := os.ReadFile("testdata/valid.pdf")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, key := range keys {
			if err := objects.Delete(cleanup, key); err != nil {
				t.Error("S3 cleanup failed")
			}
		}
	}()
	list, err := service.List(ctx, hash)
	if err != nil || list.Complete {
		t.Fatal("new draft should be incomplete")
	}
	for _, ty := range Types {
		list, err = service.Upload(ctx, hash, list.ApplicationVersion, ty.Code, "synthetic.pdf", "application/pdf", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		v, err := service.repo.Current(ctx, draft.ID, ty.Code)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, v.Key)
	}
	if !list.Complete {
		t.Fatal("four required documents should be complete")
	}
	first, err := service.repo.Current(ctx, draft.ID, "TRANSCRIPT")
	if err != nil {
		t.Fatal(err)
	}
	list, err = service.Upload(ctx, hash, list.ApplicationVersion, "TRANSCRIPT", "replacement.pdf", "application/pdf", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.repo.Current(ctx, draft.ID, "TRANSCRIPT")
	if err != nil {
		t.Fatal(err)
	}
	keys = append(keys, current.Key)
	digest := sha256.Sum256(data)
	if current.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("persisted SHA-256 incorrect")
	}
	if current.Number != 2 || current.ID == first.ID || current.Key == first.Key {
		t.Fatal("replacement version invalid")
	}
	var total, live int
	if err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE replaced_at IS NULL) FROM document_versions WHERE application_document_id=$1::uuid`, current.DocumentID).Scan(&total, &live); err != nil || total != 2 || live != 1 {
		t.Fatal("version history invalid")
	}
	old, err := objects.Get(ctx, first.Key)
	if err != nil {
		t.Fatal("old object lost")
	}
	old.Body.Close()
	v, obj, err := service.Content(ctx, hash, "TRANSCRIPT")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(obj.Body)
	obj.Body.Close()
	if err != nil || !bytes.Equal(got, data) || v.ID != current.ID {
		t.Fatal("secure get invalid")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.S3Endpoint+"/"+c.S3Bucket+"/"+current.Key, nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("object anonymously accessible")
	}
	_, otherHash, _ := drafts.NewSecret()
	_, err = drafts.NewRepository(pool).Create(ctx, "DEV_LOCAL", otherHash, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.Content(ctx, otherHash, "TRANSCRIPT"); err == nil {
		t.Fatal("IDOR")
	}
	var constraints int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='application_documents'::regclass AND conname='document_current_owner'`).Scan(&constraints); err != nil || constraints != 1 {
		t.Fatal("current version ownership constraint missing")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE document_versions SET replaced_at=now() WHERE id=$1::uuid`, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("superseded current pointer accepted")
	}
	if _, err = provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}
