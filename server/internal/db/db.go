// Package db owns the Postgres connection pool and the hand-rolled
// migration runner - no ORM, no golang-migrate dependency, matching the
// root cmaker CLI's own dependency-light ethos (see PACKS_PLAN.md §7).
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool against databaseURL and verifies it
// with a ping, so a bad connection string fails fast at startup rather
// than on the first request.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return pool, nil
}
