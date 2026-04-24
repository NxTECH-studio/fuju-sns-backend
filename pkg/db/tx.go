package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTx wraps fn inside a pgx transaction. It commits on nil error,
// rolls back otherwise. On commit failure the function returns the
// commit error; on any other error it returns the underlying error
// from fn. Rollback errors are intentionally dropped: after a
// successful Commit the rollback returns pgx.ErrTxClosed (expected
// sentinel), and on genuine abort the caller's error already describes
// the failure — logging another layer on top would just duplicate
// noise and entangle pkg/db with the application logger.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
