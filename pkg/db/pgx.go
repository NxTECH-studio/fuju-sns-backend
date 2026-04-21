// Package db exposes PostgreSQL connection helpers used by the repository
// layer. The project leans on pgx/v5 directly (no ORM, no query builder):
// NewPool hands a *pgxpool.Pool to postgres-backed repositories, and Ping
// is the health-check entry point.
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DSNConfig is the minimal set of fields NewPool needs to build a
// connection string. config.Config satisfies this shape via its DB_*
// fields + an optional DATABASE_URL env var. A dedicated interface keeps
// pkg/db decoupled from the app-level config package and easier to test.
type DSNConfig interface {
	DSN() string
}

// NewPool creates a pgx connection pool. Pool sizing defaults to pgx's
// own defaults; DB_MAX_CONNS / DB_MIN_CONNS overrides apply when set. The
// caller owns the returned pool and must call Close when shutting down.
func NewPool(ctx context.Context, cfg DSNConfig) (*pgxpool.Pool, error) {
	dsn := cfg.DSN()
	if dsn == "" {
		return nil, fmt.Errorf("db: empty DSN")
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}

	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil || n <= 0 {
			return nil, fmt.Errorf("db: invalid DB_MAX_CONNS=%q", v)
		}
		poolCfg.MaxConns = int32(n)
	}
	if v := os.Getenv("DB_MIN_CONNS"); v != "" {
		n, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil || n < 0 {
			return nil, fmt.Errorf("db: invalid DB_MIN_CONNS=%q", v)
		}
		poolCfg.MinConns = int32(n)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	// Short deadline so a bad DSN fails fast at startup rather than
	// blocking the first request.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return pool, nil
}

// Ping reports pool health with a short timeout. Safe to call from
// health endpoints.
func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pool.Ping(pingCtx)
}
