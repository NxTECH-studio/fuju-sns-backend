package main

import (
	"context"
	"fmt"

	"github.com/fuju/backend/config"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/internal/repository/postgres"
	"github.com/fuju/backend/pkg/db"
	"github.com/fuju/backend/pkg/logger"
)

// repoSet is the bundle of repository.* interfaces the server wires
// into handlers / usecases. Concrete types are interchangeable between
// the in-memory and postgres backends — every field matches the
// interface contract.
type repoSet struct {
	Users    repository.UserRepository
	Posts    repository.PostRepository
	Likes    repository.LikeRepository
	Follows  repository.FollowRepository
	Tags     repository.TagRepository
	Badges   repository.BadgeRepository
	Images   repository.ImageRepository
	OGPCache repository.OGPCacheRepository
	OGPJobs  repository.OGPJobQueue
}

// newRepositorySet constructs the repository bundle for the effective
// backend (see config.Config.RepoBackend for the selection rules).
// The returned cleanup func must be invoked at shutdown; for the
// postgres backend it closes the pgxpool, for inmemory it is a no-op.
func newRepositorySet(ctx context.Context, cfg *config.Config, log *logger.Logger) (*repoSet, func(), error) {
	switch cfg.RepoBackend() {
	case config.RepoBackendPostgres:
		pool, err := db.NewPool(ctx, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("init postgres pool: %w", err)
		}
		log.Info(ctx, "Repository backend: postgres", "max_conns", pool.Config().MaxConns)
		store := postgres.New(pool)
		return &repoSet{
			Users:    store.Users,
			Posts:    store.Posts,
			Likes:    store.Likes,
			Follows:  store.Follows,
			Tags:     store.Tags,
			Badges:   store.Badges,
			Images:   store.Images,
			OGPCache: store.OGPCache,
			OGPJobs:  store.OGPJobs,
		}, pool.Close, nil

	case config.RepoBackendInMemory:
		log.Info(ctx, "Repository backend: inmemory")
		links := inmemory.NewLinkStore()
		return &repoSet{
			Users:    inmemory.NewUserRepository(),
			Posts:    inmemory.NewPostRepository(links),
			Likes:    inmemory.NewLikeRepository(),
			Follows:  inmemory.NewFollowRepository(),
			Tags:     inmemory.NewTagRepository(links),
			Badges:   inmemory.NewBadgeRepository(),
			Images:   inmemory.NewImageRepository(links),
			OGPCache: inmemory.NewOGPCacheRepository(links),
			OGPJobs:  inmemory.NewOGPJobQueue(),
		}, func() {}, nil

	default:
		// RepoBackend() already normalises unknown values, so this
		// branch is defensive only — if it ever fires, something
		// changed in the config package.
		return nil, nil, fmt.Errorf("unknown repository backend: %q", cfg.RepoBackend())
	}
}
