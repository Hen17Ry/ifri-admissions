package database

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, connection string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connection)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 5
	return pgxpool.NewWithConfig(ctx, config)
}
