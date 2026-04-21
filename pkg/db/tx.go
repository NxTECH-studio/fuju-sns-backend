package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTx wraps fn inside a pgx transaction. It commits on nil error,
// rolls back otherwise. On commit failure the function returns the
// commit error; on any other error it returns the underlying error from
// fn (rollback errors are ignored because the transaction aborts either
// way and surfacing them hides the real cause).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful Commit and returns
		// pgx.ErrTxClosed. Drop that sentinel silently.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			_ = rbErr
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
