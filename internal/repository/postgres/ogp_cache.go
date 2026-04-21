package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/fuju/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OGPCacheRepository is the postgres-backed
// repository.OGPCacheRepository. Schema lives in migration 007
// (ogp_cache + post_ogp).
type OGPCacheRepository struct {
	pool *pgxpool.Pool
}

// NewOGPCacheRepository constructs an OGPCacheRepository bound to pool.
func NewOGPCacheRepository(pool *pgxpool.Pool) *OGPCacheRepository {
	return &OGPCacheRepository{pool: pool}
}

const ogpCacheSelectColumns = `url_hash, url, title, description, image_url,
	site_name, canonical_url, fetched_at, expires_at, status, error_reason`

// ogpCacheJoinColumns mirrors ogpCacheSelectColumns aliased as `c.` for
// the post_ogp JOIN. Same pattern as badgeJoinColumns / imageJoinColumns.
const ogpCacheJoinColumns = `c.url_hash, c.url, c.title, c.description, c.image_url,
	c.site_name, c.canonical_url, c.fetched_at, c.expires_at, c.status, c.error_reason`

func scanOGPPreview(row pgx.Row) (*domain.OGPPreview, error) {
	var p domain.OGPPreview
	err := row.Scan(
		&p.URLHash,
		&p.URL,
		&p.Title,
		&p.Description,
		&p.ImageURL,
		&p.SiteName,
		&p.CanonicalURL,
		&p.FetchedAt,
		&p.ExpiresAt,
		&p.Status,
		&p.ErrorReason,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Get returns (nil, nil) on cache miss. Expired rows are returned as-is
// so the caller can decide to use them as a grace fallback; only the
// miss path gates on existence.
func (r *OGPCacheRepository) Get(ctx context.Context, urlHash string) (*domain.OGPPreview, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+ogpCacheSelectColumns+`
		FROM ogp_cache
		WHERE url_hash = $1`, urlHash)
	p, err := scanOGPPreview(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get ogp cache: %w", err)
	}
	return p, nil
}

// Upsert inserts or fully replaces the cache row keyed by url_hash.
// A zero FetchedAt is substituted with NOW() to match the schema's
// DEFAULT; ExpiresAt is always caller-supplied (no sensible default
// beyond "now" would be correct — callers decide the TTL).
func (r *OGPCacheRepository) Upsert(ctx context.Context, preview *domain.OGPPreview) error {
	if preview == nil || preview.URLHash == "" {
		return nil
	}
	var fetchedAtArg any
	if !preview.FetchedAt.IsZero() {
		fetchedAtArg = preview.FetchedAt
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ogp_cache (url_hash, url, title, description, image_url,
		                       site_name, canonical_url, fetched_at, expires_at,
		                       status, error_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()), $9, $10, $11)
		ON CONFLICT (url_hash) DO UPDATE SET
			url           = EXCLUDED.url,
			title         = EXCLUDED.title,
			description   = EXCLUDED.description,
			image_url     = EXCLUDED.image_url,
			site_name     = EXCLUDED.site_name,
			canonical_url = EXCLUDED.canonical_url,
			fetched_at    = EXCLUDED.fetched_at,
			expires_at    = EXCLUDED.expires_at,
			status        = EXCLUDED.status,
			error_reason  = EXCLUDED.error_reason`,
		preview.URLHash, preview.URL, preview.Title, preview.Description,
		preview.ImageURL, preview.SiteName, preview.CanonicalURL,
		fetchedAtArg, preview.ExpiresAt, preview.Status, preview.ErrorReason,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert ogp cache: %w", err)
	}
	return nil
}

// ListByPostIDs resolves every post_ogp → ogp_cache chain for the
// given posts, returning previews grouped by post ID and ordered by
// position ASC within each group.
func (r *OGPCacheRepository) ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.OGPPreview, error) {
	out := make(map[string][]*domain.OGPPreview, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT po.post_id, `+ogpCacheJoinColumns+`
		FROM ogp_cache c
		JOIN post_ogp po ON po.url_hash = c.url_hash
		WHERE po.post_id = ANY($1)
		ORDER BY po.post_id, po.position ASC`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list ogp by posts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var postID string
		var p domain.OGPPreview
		if err := rows.Scan(&postID,
			&p.URLHash, &p.URL, &p.Title, &p.Description, &p.ImageURL,
			&p.SiteName, &p.CanonicalURL, &p.FetchedAt, &p.ExpiresAt,
			&p.Status, &p.ErrorReason,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan ogp preview: %w", err)
		}
		cp := p
		out[postID] = append(out[postID], &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list ogp by posts iterate: %w", err)
	}
	return out, nil
}
