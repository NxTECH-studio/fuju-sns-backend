// Package ogp contains OGP-feature use cases: the post-commit enqueue
// hook and the background fetch worker. pkg/ogp owns the normalization
// / fetch / parse primitives; this package coordinates persistence.
package ogp

import (
	"context"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/ogp"
	"github.com/oklog/ulid/v2"
)

// Enqueuer runs after a post is committed and either attaches an
// already-cached OGP preview or enqueues a background fetch job.
//
// All failures are logged and swallowed: the post commit has already
// landed, and the user-visible outcome must not depend on the OGP
// pipeline being healthy.
type Enqueuer struct {
	cache repository.OGPCacheRepository
	queue repository.OGPJobQueue
	posts repository.PostRepository
	log   *logger.Logger
	now   func() time.Time
}

// NewEnqueuer builds an Enqueuer. log may be nil in tests.
func NewEnqueuer(
	cache repository.OGPCacheRepository,
	queue repository.OGPJobQueue,
	posts repository.PostRepository,
	log *logger.Logger,
) *Enqueuer {
	return &Enqueuer{cache: cache, queue: queue, posts: posts, log: log, now: time.Now}
}

// EnqueueForPost extracts URLs from the post's content, resolves each
// through the cache, and attaches or enqueues appropriately. Errors
// are logged but never returned — the caller (post-commit hook) must
// always succeed.
func (e *Enqueuer) EnqueueForPost(ctx context.Context, post *domain.Post) {
	if post == nil || post.Content == "" {
		return
	}
	urls := ogp.ExtractURLs(post.Content)
	if len(urls) == 0 {
		return
	}

	now := e.now()
	for position, raw := range urls {
		normalized, err := ogp.Normalize(raw)
		if err != nil {
			e.warn(ctx, "ogp normalize failed", "url", raw, "err", err)
			continue
		}
		urlHash := ogp.Hash(normalized)

		cached, err := e.cache.Get(ctx, urlHash)
		if err != nil {
			e.warn(ctx, "ogp cache lookup failed", "url_hash", urlHash, "err", err)
			// Fall through to the enqueue branch so we still try to
			// fetch. The worker re-checks the cache on dequeue.
			cached = nil
		}
		if cached != nil && !cached.IsExpired(now) {
			// Fresh cache. On success, attach. On a fresh error row,
			// skip: re-enqueueing within the 30-minute error TTL would
			// just burn worker cycles re-hitting a URL we already know
			// is broken.
			if cached.Status == domain.OGPStatusOK {
				if err := e.posts.AttachOGP(ctx, post.ID, urlHash, position); err != nil {
					e.warn(ctx, "ogp attach failed", "post_id", post.ID, "err", err)
				}
			}
			continue
		}

		id := ulid.Make().String()
		if err := e.queue.Enqueue(ctx, id, urlHash, normalized, post.ID, position); err != nil {
			e.warn(ctx, "ogp enqueue failed", "post_id", post.ID, "url", normalized, "err", err)
		}
	}
}

// warn logs at WARN level when a logger is configured. Tests pass nil
// and rely on the no-log branch.
func (e *Enqueuer) warn(ctx context.Context, msg string, kv ...any) {
	if e.log == nil {
		return
	}
	e.log.Warn(ctx, msg, kv...)
}
