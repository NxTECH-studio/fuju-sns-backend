// Package db exposes PostgreSQL connection helpers used by the repository
// layer. The project leans on pgx/v5 directly (no ORM, no query builder):
// NewPool hands a *pgxpool.Pool to postgres-backed repositories, and Ping
// is the health-check entry point.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig is everything NewPool needs to stand up a pgxpool. The
// interface keeps pkg/db decoupled from config.Config — any caller
// that can supply a DSN and pool sizing hints is welcome. Zero values
// for MaxConns / MinConns mean "inherit the pgx default".
type PoolConfig interface {
	DSN() string
	MaxConns() int32
	MinConns() int32
}

// NewPool creates a pgx connection pool. The caller owns the returned
// pool and must call Close when shutting down.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	dsn := cfg.DSN()
	if dsn == "" {
		return nil, fmt.Errorf("db: empty DSN")
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}

	if n := cfg.MaxConns(); n > 0 {
		poolCfg.MaxConns = n
	}
	if n := cfg.MinConns(); n > 0 {
		poolCfg.MinConns = n
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
