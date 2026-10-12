package seeds

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed references.sql development.sql
var files embed.FS

func Development(ctx context.Context, pool *pgxpool.Pool, environment string) error {
	if environment != "development" && environment != "test" {
		return fmt.Errorf("development seed requires APP_ENV=development or test")
	}
	canonical, err := files.ReadFile("references.sql")
	if err != nil {
		return err
	}
	fixtures, err := files.ReadFile("development.sql")
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize repeated/concurrent seed executions without overwriting published rules.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73492217)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, string(canonical)+"\n"+string(fixtures)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
