//go:build integration

package drafts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

func TestPostgresDraftLifecycle(t *testing.T) {
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, c.DatabaseURL())
	if err != nil {
		t.Fatal("database configuration invalid")
	}
	t.Cleanup(admin.Close)
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	schema := "ifri_draft_test_" + hex.EncodeToString(token)
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
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err = seeds.Development(ctx, pool, "test"); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool)
	secret, hash, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	d, err := repo.Create(ctx, "DEV_LOCAL", hash, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("atomic creation and hashed session", func(t *testing.T) {
		if d.State != "DRAFT" || d.Program != nil || d.Qualification != nil {
			t.Fatal("invalid draft")
		}
		var stored []byte
		if err = pool.QueryRow(ctx, `SELECT secret_hash FROM application_draft_sessions WHERE application_id=$1::uuid`, d.ID).Scan(&stored); err != nil || string(stored) == secret || string(stored) != string(hash) {
			t.Fatal("session hash invalid")
		}
		var before, after int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM candidates`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Create(ctx, "DEV_LOCAL", hash, time.Hour); err == nil {
			t.Fatal("duplicate hash accepted")
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM candidates`).Scan(&after); err != nil || before != after {
			t.Fatal("creation was not atomic")
		}
	})
	t.Run("session ownership and unauthorized access", func(t *testing.T) {
		restored, err := repo.Current(ctx, hash)
		if err != nil || restored.ID != d.ID {
			t.Fatal("restore failed")
		}
		if _, err = repo.Current(ctx, HashSecret("unknown")); !errors.Is(err, ErrSession) {
			t.Fatal("missing session allowed")
		}
		_, otherHash, _ := NewSecret()
		other, err := repo.Create(ctx, "DEV_LOCAL", otherHash, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		got, err := repo.Current(ctx, otherHash)
		if err != nil || got.ID != other.ID || got.ID == d.ID {
			t.Fatal("ownership violation")
		}
	})
	t.Run("profile and validation", func(t *testing.T) {
		p := Profile{" Dossou ", " Élise ", " TEST@Example.COM ", "+33 1 23 45 67 89"}
		d, err = repo.Update(ctx, hash, "profile", Change{Version: d.Version, Profile: &p})
		if err != nil {
			t.Fatal(err)
		}
		if d.Profile.Email != "test@example.com" || d.Profile.FirstNames != "Élise" || d.Profile.Phone != "+33123456789" {
			t.Fatal("normalization failed")
		}
		invalid := Profile{Email: "invalid"}
		if _, err = repo.Update(ctx, hash, "profile", Change{Version: d.Version, Profile: &invalid}); err == nil {
			t.Fatal("invalid email accepted")
		}
		if _, err = repo.Update(ctx, hash, "profile", Change{Version: 1, Profile: &p}); !errors.Is(err, ErrConflict) {
			t.Fatal("stale version accepted")
		}
	})
	year := time.Now().UTC().Year() - 1
	t.Run("BAC E and DT DWM eligible; DT EAP clears GL", func(t *testing.T) {
		for _, q := range []string{"BAC_E", "DT_DWM"} {
			d, err = repo.Update(ctx, hash, "academic-background", Change{Version: d.Version, Qualification: q, GraduationYear: &year})
			if err != nil {
				t.Fatal(err)
			}
			d, err = repo.Update(ctx, hash, "program", Change{Version: d.Version, Program: "GL"})
			if err != nil || d.Program == nil || d.Program.Code != "GL" {
				t.Fatal("eligible selection failed", err)
			}
		}
		d, err = repo.Update(ctx, hash, "academic-background", Change{Version: d.Version, Qualification: "DT_EAP", GraduationYear: &year})
		if err != nil || d.Program != nil {
			t.Fatal("GL was not cleared", err)
		}
		if _, err = repo.Update(ctx, hash, "program", Change{Version: d.Version, Program: "GL"}); !errors.Is(err, ErrIneligible) {
			t.Fatal("ineligible GL accepted")
		}
		future := time.Now().UTC().Year() + 1
		if _, err = repo.Update(ctx, hash, "academic-background", Change{Version: d.Version, Qualification: "DT_EAP", GraduationYear: &future}); err == nil {
			t.Fatal("future year accepted")
		}
		if _, err = repo.Update(ctx, hash, "academic-background", Change{Version: d.Version, Qualification: "UNKNOWN", GraduationYear: &year}); err == nil {
			t.Fatal("unknown qualification accepted")
		}
	})
	t.Run("closed campaign prevents creation and mutation", func(t *testing.T) {
		if _, err = pool.Exec(ctx, `UPDATE admission_campaigns SET status='CLOSED' WHERE code='DEV_LOCAL'`); err != nil {
			t.Fatal(err)
		}
		_, fresh, _ := NewSecret()
		if _, err = repo.Create(ctx, "DEV_LOCAL", fresh, time.Hour); err == nil {
			t.Fatal("closed creation accepted")
		}
		if _, err = repo.Update(ctx, hash, "profile", Change{Version: d.Version, Profile: &d.Profile}); !errors.Is(err, ErrClosed) {
			t.Fatal("closed mutation accepted")
		}
		if _, err = pool.Exec(ctx, `UPDATE admission_campaigns SET status='OPEN' WHERE code='DEV_LOCAL'`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("expiration and revocation", func(t *testing.T) {
		if _, err = pool.Exec(ctx, `UPDATE application_draft_sessions SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE secret_hash=$1`, hash); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Current(ctx, hash); !errors.Is(err, ErrExpired) {
			t.Fatal("expired session accepted")
		}
		if _, err = pool.Exec(ctx, `UPDATE application_draft_sessions SET revoked_at=now() WHERE secret_hash=$1`, hash); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Current(ctx, hash); !errors.Is(err, ErrRevoked) {
			t.Fatal("revoked session accepted")
		}
	})
	t.Run("constraints and rollback reapply", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE applications SET state='OCR_VALIDATED' WHERE id=$1::uuid`, d.ID); err == nil {
			t.Fatal("mixed state accepted")
		}
		_ = tx.Rollback(ctx)
		if _, err = provider.DownTo(ctx, 2); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name='applications'`, schema).Scan(&count); err != nil || count != 0 {
			t.Fatal("rollback failed")
		}
		if _, err = provider.Up(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
