package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/platform/database"
	"ifri-admissions/services/api/migrations"
	"ifri-admissions/services/api/seeds"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Database command failed; check configuration, connectivity and schema (credentials omitted).")
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) {
			fmt.Fprintln(os.Stderr, "PostgreSQL code:", pgerr.Code, "constraint:", pgerr.ConstraintName)
		}
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: db up|down|seed")
	}
	action := os.Args[1]
	if action != "up" && action != "down" && action != "seed" {
		return fmt.Errorf("unknown database command")
	}
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Configuration:", err)
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if action == "seed" {
		pool, err := database.Open(ctx, c.DatabaseURL())
		if err != nil {
			return err
		}
		defer pool.Close()
		if err = seeds.Development(ctx, pool, c.Environment); err != nil {
			return err
		}
	} else {
		db, err := sql.Open("pgx", c.DatabaseURL())
		if err != nil {
			return err
		}
		defer db.Close()
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return err
		}
		provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files, goose.WithSessionLocker(locker))
		if err != nil {
			return err
		}
		if action == "up" {
			_, err = provider.Up(ctx)
		} else {
			_, err = provider.Down(ctx)
		}
		if err != nil {
			return err
		}
	}
	fmt.Println("Database command", action, ": PASS")
	return nil
}
