// Package postgres implements the repository.* interfaces on top of
// PostgreSQL via pgx/v5. The in-memory implementation in
// internal/repository/inmemory remains authoritative for unit tests;
// this package is exercised by contract tests under the "integration"
// build tag (see internal/repository/testsupport).
package postgres

import (
	"github.com/fuju/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Compile-time assertions that postgres types implement the
// corresponding repository.* interfaces. Phase 3 closes out the
// remaining repositories: Images / OGPCache / OGPJobs.
var (
	_ repository.UserRepository     = (*UserRepository)(nil)
	_ repository.PostRepository     = (*PostRepository)(nil)
	_ repository.LikeRepository     = (*LikeRepository)(nil)
	_ repository.FollowRepository   = (*FollowRepository)(nil)
	_ repository.TagRepository      = (*TagRepository)(nil)
	_ repository.BadgeRepository    = (*BadgeRepository)(nil)
	_ repository.ImageRepository    = (*ImageRepository)(nil)
	_ repository.OGPCacheRepository = (*OGPCacheRepository)(nil)
	_ repository.OGPJobQueue        = (*OGPJobQueue)(nil)
)

// Store aggregates every postgres-backed repository so cmd/server can
// wire them in a single call. With Phase 3 the struct covers all
// repository.* interfaces; Phase 4 will wire this into the server.
type Store struct {
	Users    *UserRepository
	Posts    *PostRepository
	Likes    *LikeRepository
	Follows  *FollowRepository
	Tags     *TagRepository
	Badges   *BadgeRepository
	Images   *ImageRepository
	OGPCache *OGPCacheRepository
	OGPJobs  *OGPJobQueue

	pool *pgxpool.Pool
}

// New constructs a Store with the supplied pool. The pool is retained
// on the Store so callers may reach Close / health methods through it.
func New(pool *pgxpool.Pool) *Store {
	return &Store{
		Users:    NewUserRepository(pool),
		Posts:    NewPostRepository(pool),
		Likes:    NewLikeRepository(pool),
		Follows:  NewFollowRepository(pool),
		Tags:     NewTagRepository(pool),
		Badges:   NewBadgeRepository(pool),
		Images:   NewImageRepository(pool),
		OGPCache: NewOGPCacheRepository(pool),
		OGPJobs:  NewOGPJobQueue(pool),
		pool:     pool,
	}
}

// Pool returns the underlying connection pool. Useful for health
// checks and integration tests that need raw TRUNCATE access.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}
