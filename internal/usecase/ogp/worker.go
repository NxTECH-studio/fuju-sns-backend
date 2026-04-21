package ogp

import (
	"context"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/ogp"
)

// Default TTLs for the success / error paths. Kept at package scope so
// tests can observe them on the Upserted rows.
const (
	SuccessTTL    = 3 * 24 * time.Hour
	ErrorTTL      = 30 * time.Minute
	MaxAttempts   = 3
	ClaimInterval = time.Second // sleep between Claim misses
)

// Worker is the background goroutine that drains OGPJobQueue, fetches
// OGP metadata, caches the result, and attaches it to the originating
// post. It's designed to run as a single in-process goroutine; adding
// more workers is a drop-in change because the queue is claim-safe.
type Worker struct {
	queue   repository.OGPJobQueue
	cache   repository.OGPCacheRepository
	posts   repository.PostRepository
	fetcher *ogp.Fetcher
	log     *logger.Logger

	workerID string
	now      func() time.Time

	// interval overrides the Claim miss sleep for tests so they don't
	// spin on a real second.
	interval time.Duration
}

// NewWorker builds a Worker. log and fetcher must be non-nil.
func NewWorker(
	queue repository.OGPJobQueue,
	cache repository.OGPCacheRepository,
	posts repository.PostRepository,
	fetcher *ogp.Fetcher,
	log *logger.Logger,
) *Worker {
	return &Worker{
		queue:    queue,
		cache:    cache,
		posts:    posts,
		fetcher:  fetcher,
		log:      log,
		workerID: "ogp-worker",
		now:      time.Now,
		interval: ClaimInterval,
	}
}

// Run blocks until ctx is cancelled. Each iteration claims at most one
// job; empty claims back off by w.interval so the loop doesn't spin.
func (w *Worker) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		w.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
	}
}

// runOnce attempts one Claim → process cycle. A Claim miss sleeps
// w.interval before returning so the caller's Run loop can re-enter
// without busy-waiting. Extracted for testability.
func (w *Worker) runOnce(ctx context.Context) {
	job, err := w.queue.Claim(ctx, w.workerID)
	if err != nil {
		w.warn(ctx, "ogp claim failed", "err", err)
		w.sleep(ctx)
		return
	}
	if job == nil {
		w.sleep(ctx)
		return
	}
	w.process(ctx, job)
}

// process runs the fetch + cache + attach pipeline for one job. On
// failure it routes between retriable and non-retriable paths.
func (w *Worker) process(ctx context.Context, job *domain.OGPJob) {
	preview, err := w.fetcher.Fetch(ctx, job.URL)
	if err == nil {
		now := w.now()
		preview.Status = domain.OGPStatusOK
		preview.ExpiresAt = now.Add(SuccessTTL)
		if upsertErr := w.cache.Upsert(ctx, preview); upsertErr != nil {
			w.warn(ctx, "ogp cache upsert failed", "job_id", job.ID, "err", upsertErr)
			w.markFailed(ctx, job.ID, upsertErr.Error(), true)
			return
		}
		if job.PostID != "" {
			if attachErr := w.posts.AttachOGP(ctx, job.PostID, preview.URLHash, 0); attachErr != nil {
				w.warn(ctx, "ogp attach failed", "job_id", job.ID, "err", attachErr)
			}
		}
		if doneErr := w.queue.MarkDone(ctx, job.ID); doneErr != nil {
			w.warn(ctx, "ogp mark done failed", "job_id", job.ID, "err", doneErr)
		}
		return
	}

	if ogp.Retriable(err) && job.Attempts < MaxAttempts {
		w.warn(ctx, "ogp fetch failed (retriable)", "job_id", job.ID, "attempts", job.Attempts, "err", err)
		w.markFailed(ctx, job.ID, err.Error(), true)
		return
	}

	// Non-retriable or attempt budget exhausted: write a short-TTL
	// error row so future enqueues short-circuit without hitting the
	// network again, then finalize the job.
	w.warn(ctx, "ogp fetch failed (terminal)", "job_id", job.ID, "err", err)
	now := w.now()
	errPreview := &domain.OGPPreview{
		URLHash:     job.URLHash,
		URL:         job.URL,
		FetchedAt:   now,
		ExpiresAt:   now.Add(ErrorTTL),
		Status:      domain.OGPStatusError,
		ErrorReason: truncate(err.Error(), 255),
	}
	if upsertErr := w.cache.Upsert(ctx, errPreview); upsertErr != nil {
		w.warn(ctx, "ogp error cache upsert failed", "job_id", job.ID, "err", upsertErr)
	}
	w.markFailed(ctx, job.ID, err.Error(), false)
}

// markFailed wraps MarkFailed so queue-side errors get logged. A failure
// to flip the job status doesn't stop processing — the job simply ages
// in the queue until a future sweep — but it's worth surfacing.
func (w *Worker) markFailed(ctx context.Context, jobID, reason string, retriable bool) {
	if err := w.queue.MarkFailed(ctx, jobID, reason, retriable); err != nil {
		w.warn(ctx, "ogp mark failed persistence error", "job_id", jobID, "retriable", retriable, "err", err)
	}
}

// sleep returns early if ctx is cancelled; otherwise it blocks for
// w.interval. Tests set w.interval to 0 to avoid wall-clock waits.
func (w *Worker) sleep(ctx context.Context) {
	if w.interval <= 0 {
		return
	}
	t := time.NewTimer(w.interval)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (w *Worker) warn(ctx context.Context, msg string, kv ...any) {
	if w.log == nil {
		return
	}
	w.log.Warn(ctx, msg, kv...)
}

// truncate caps s to maxLen bytes. Cheap: OGP error rows have a 255-char
// column.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
