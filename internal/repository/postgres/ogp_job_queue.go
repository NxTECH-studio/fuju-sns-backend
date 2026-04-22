package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/fuju/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OGPJobQueue is the postgres-backed repository.OGPJobQueue. The
// schema is ogp_jobs (migration 007 + 008). Claim uses the standard
// `FOR UPDATE SKIP LOCKED` pattern so multiple workers can drive the
// queue without serializing on a single leader.
type OGPJobQueue struct {
	pool *pgxpool.Pool
}

// NewOGPJobQueue constructs an OGPJobQueue bound to pool.
func NewOGPJobQueue(pool *pgxpool.Pool) *OGPJobQueue {
	return &OGPJobQueue{pool: pool}
}

const ogpJobSelectColumns = `ogp_jobs.id, ogp_jobs.url_hash, ogp_jobs.url,
	ogp_jobs.post_id, ogp_jobs.position, ogp_jobs.enqueued_at,
	ogp_jobs.started_at, ogp_jobs.finished_at, ogp_jobs.status,
	ogp_jobs.attempts, ogp_jobs.last_error`

func scanOGPJob(row pgx.Row) (*domain.OGPJob, error) {
	var j domain.OGPJob
	var postID *string
	err := row.Scan(
		&j.ID,
		&j.URLHash,
		&j.URL,
		&postID,
		&j.Position,
		&j.EnqueuedAt,
		&j.StartedAt,
		&j.FinishedAt,
		&j.Status,
		&j.Attempts,
		&j.LastError,
	)
	if err != nil {
		return nil, err
	}
	if postID != nil {
		j.PostID = *postID
	}
	return &j, nil
}

// Enqueue adds a queued job. Duplicate ids trigger a PK conflict; we
// silently swallow them so the Enqueuer remains idempotent under
// retries — matches inmemory's "duplicate id is a no-op" behaviour.
func (r *OGPJobQueue) Enqueue(ctx context.Context, id, urlHash, url, postID string, position int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ogp_jobs (id, url_hash, url, post_id, position,
		                     enqueued_at, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, NOW(), 'queued', 0, '')
		ON CONFLICT (id) DO NOTHING`,
		id, urlHash, url, postID, position)
	if err != nil {
		return fmt.Errorf("postgres: enqueue ogp job: %w", err)
	}
	return nil
}

// Claim atomically moves the oldest queued job to running under
// FOR UPDATE SKIP LOCKED semantics — two concurrent Claim calls will
// always return distinct jobs (or one returns (nil, nil) when only
// one job was queued). The workerID argument is preserved for
// interface parity with the in-memory impl and for future
// claimed_by / claimed_at columns; the current schema has no such
// column so we do not persist it.
func (r *OGPJobQueue) Claim(ctx context.Context, _ string) (*domain.OGPJob, error) {
	const q = `
	WITH next AS (
		SELECT id FROM ogp_jobs
		WHERE status = 'queued'
		ORDER BY enqueued_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	)
	UPDATE ogp_jobs
	SET status     = 'running',
	    started_at = NOW(),
	    attempts   = ogp_jobs.attempts + 1
	FROM next
	WHERE ogp_jobs.id = next.id
	RETURNING ` + ogpJobSelectColumns
	row := r.pool.QueryRow(ctx, q)
	j, err := scanOGPJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Empty queue — the CTE selected no rows so the UPDATE
			// never ran and RETURNING yielded nothing.
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: claim ogp job: %w", err)
	}
	return j, nil
}

// MarkDone transitions the claimed job to done. The caller is trusted
// to only call this with a jobID they claimed.
func (r *OGPJobQueue) MarkDone(ctx context.Context, jobID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ogp_jobs SET
			status      = 'done',
			finished_at = NOW()
		WHERE id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("postgres: mark ogp job done: %w", err)
	}
	return nil
}

// MarkFailed records a failure on a claimed job. Retriable failures
// send the job back to `queued` with last_error populated so the
// next Claim can retry; non-retriable failures are finalized as
// `failed` and never reclaimed. started_at is cleared on retry so
// "time in running" reflects the next attempt, matching inmemory.
func (r *OGPJobQueue) MarkFailed(ctx context.Context, jobID, reason string, retriable bool) error {
	if retriable {
		_, err := r.pool.Exec(ctx, `
			UPDATE ogp_jobs SET
				status     = 'queued',
				started_at = NULL,
				last_error = $2
			WHERE id = $1`, jobID, reason)
		if err != nil {
			return fmt.Errorf("postgres: requeue ogp job: %w", err)
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE ogp_jobs SET
			status      = 'failed',
			finished_at = NOW(),
			last_error  = $2
		WHERE id = $1`, jobID, reason)
	if err != nil {
		return fmt.Errorf("postgres: fail ogp job: %w", err)
	}
	return nil
}
