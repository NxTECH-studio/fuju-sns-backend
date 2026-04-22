package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/fuju/backend/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// TagRepository is the postgres-backed repository.TagRepository. Tag
// IDs are ULIDs generated in the repository when a brand-new name is
// inserted — callers need not pre-generate them. Names are expected
// pre-normalized (lower-cased, trimmed) by the caller so UNIQUE(name)
// has its intended effect.
type TagRepository struct {
	pool *pgxpool.Pool
}

// NewTagRepository constructs a TagRepository bound to pool.
func NewTagRepository(pool *pgxpool.Pool) *TagRepository {
	return &TagRepository{pool: pool}
}

// UpsertByNames inserts any missing name and returns the full Tag set
// in input order (after dropping empty strings and duplicates). New
// rows get a freshly-minted ULID; existing rows keep their stored ID
// and created_at. Atomicity: the whole batch runs in a single
// INSERT ... ON CONFLICT statement per name, which is cheap for the
// <= MaxTagsPerPost (10) expected batch size.
func (r *TagRepository) UpsertByNames(ctx context.Context, names []string) ([]*domain.Tag, error) {
	// De-duplicate preserving order.
	seen := make(map[string]struct{}, len(names))
	unique := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		unique = append(unique, n)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	out := make([]*domain.Tag, 0, len(unique))
	for _, name := range unique {
		// Pre-generate the ID; ON CONFLICT DO UPDATE ... RETURNING
		// gives us the effective row (existing or new) without a
		// separate SELECT round-trip.
		proposedID := ulid.Make().String()
		row := r.pool.QueryRow(ctx, `
			INSERT INTO tags (id, name, created_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id, name, created_at`,
			proposedID, name)
		var t domain.Tag
		if err := row.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: upsert tag %q: %w", name, err)
		}
		out = append(out, &t)
	}
	return out, nil
}

// ListByPostID returns tags attached to postID, ordered by name
// ascending to match inmemory.
func (r *TagRepository) ListByPostID(ctx context.Context, postID string) ([]*domain.Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.name, t.created_at
		FROM tags t
		JOIN post_tags pt ON pt.tag_id = t.id
		WHERE pt.post_id = $1
		ORDER BY t.name ASC`, postID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list tags by post: %w", err)
	}
	defer rows.Close()

	var out []*domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan tag: %w", err)
		}
		out = append(out, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list tags by post iterate: %w", err)
	}
	return out, nil
}

// ListByPostIDs batches ListByPostID across posts. Posts with no tags
// are absent from the returned map. Grouping is done in Go from a
// single JOIN query.
func (r *TagRepository) ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.Tag, error) {
	out := make(map[string][]*domain.Tag, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT pt.post_id, t.id, t.name, t.created_at
		FROM tags t
		JOIN post_tags pt ON pt.tag_id = t.id
		WHERE pt.post_id = ANY($1)`, postIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list tags by posts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var postID string
		var t domain.Tag
		if err := rows.Scan(&postID, &t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan post tag: %w", err)
		}
		cp := t
		out[postID] = append(out[postID], &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list tags by posts iterate: %w", err)
	}

	for postID := range out {
		sort.Slice(out[postID], func(i, j int) bool {
			return out[postID][i].Name < out[postID][j].Name
		})
	}
	return out, nil
}
