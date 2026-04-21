// Package postgres implements the repository.* interfaces on top of
// PostgreSQL via pgx/v5. The in-memory implementation in
// internal/repository/inmemory remains authoritative for unit tests;
// this package is exercised by contract tests under the "integration"
// build tag (see internal/repository/testsupport).
package postgres

import (
	"errors"

	"github.com/fuju/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Compile-time assertions that postgres types implement the
// corresponding repository.* interfaces. Phase 1 covers Users / Posts
// / Likes; later phases extend this block.
var (
	_ repository.UserRepository = (*UserRepository)(nil)
	_ repository.PostRepository = (*PostRepository)(nil)
	_ repository.LikeRepository = (*LikeRepository)(nil)
)

// Store aggregates every postgres-backed repository so cmd/server can
// wire them in a single call. Phase 1 only populates Users / Posts /
// Likes; later phases grow this struct as Follow / Tag / Image / Badge
// / OGP repositories land.
type Store struct {
	Users *UserRepository
	Posts *PostRepository
	Likes *LikeRepository

	pool *pgxpool.Pool
}

// New constructs a Store with the supplied pool. The pool is retained
// on the Store so callers may reach Close / health methods through it.
func New(pool *pgxpool.Pool) *Store {
	return &Store{
		Users: NewUserRepository(pool),
		Posts: NewPostRepository(pool),
		Likes: NewLikeRepository(pool),
		pool:  pool,
	}
}

// Pool returns the underlying connection pool. Useful for health
// checks and integration tests that need raw TRUNCATE access.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// uniqueViolation reports whether err is a postgres UNIQUE constraint
// violation (SQLSTATE 23505). Repositories that treat uniqueness
// conflicts as idempotent no-ops (e.g. LikeRepository.Create, AttachOGP)
// call this to distinguish "already there" from a real error.
func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
