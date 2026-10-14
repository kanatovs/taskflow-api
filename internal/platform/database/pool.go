package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a lazy pool. Database outages must not prevent HTTP liveness.
func New(ctx context.Context, databaseURL string, timeout time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Parser errors can contain credentials from the connection string.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	cfg.ConnConfig.ConnectTimeout = timeout
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("create PostgreSQL connection pool")
	}
	return pool, nil
}
