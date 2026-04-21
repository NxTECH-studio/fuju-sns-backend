package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fuju/backend/internal/domain"
	pkgdb "github.com/fuju/backend/pkg/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostRepository is the postgres-backed repository.PostRepository.
type PostRepository struct {
	pool *pgxpool.Pool
}

// NewPostRepository constructs a PostRepository bound to pool.
func NewPostRepository(pool *pgxpool.Pool) *PostRepository {
	return &PostRepository{pool: pool}
}

const postSelectColumns = `id, user_id, content, parent_post_id, root_post_id,
	likes_count, replies_count, visibility,
	created_at, updated_at, deleted_at`

// scanPost maps a row ordered as postSelectColumns into a domain.Post.
func scanPost(row pgx.Row) (*domain.Post, error) {
	var p domain.Post
	err := row.Scan(
		&p.ID,
		&p.UserID,
		&p.Content,
		&p.ParentPostID,
		&p.RootPostID,
		&p.LikesCount,
		&p.RepliesCount,
		&p.Visibility,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetByID returns (nil, nil) for missing or soft-deleted posts.
func (r *PostRepository) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+postSelectColumns+`
		FROM posts
		WHERE id = $1 AND deleted_at IS NULL`, id)
	p, err := scanPost(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get post: %w", err)
	}
	return p, nil
}

// Create writes the post + its post_images + post_tags rows atomically.
// Caller supplies the post ID (ULID) and the image/tag IDs. CreatedAt /
// UpdatedAt are always set to NOW() so callers can leave them zero.
func (r *PostRepository) Create(ctx context.Context, post *domain.Post, imageIDs []string, tagIDs []string) (*domain.Post, error) {
	var out *domain.Post
	err := pkgdb.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		visibility := post.Visibility
		if visibility == "" {
			visibility = "public"
		}

		row := tx.QueryRow(ctx, `
			INSERT INTO posts (id, user_id, content, parent_post_id, root_post_id,
			                   likes_count, replies_count, visibility,
			                   created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			RETURNING `+postSelectColumns,
			post.ID,
			post.UserID,
			post.Content,
			post.ParentPostID,
			post.RootPostID,
			post.LikesCount,
			post.RepliesCount,
			visibility,
		)
		p, err := scanPost(row)
		if err != nil {
			return fmt.Errorf("postgres: insert post: %w", err)
		}

		for i, imgID := range imageIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO post_images (post_id, image_id, position) VALUES ($1, $2, $3)`,
				p.ID, imgID, i,
			); err != nil {
				return fmt.Errorf("postgres: attach image: %w", err)
			}
		}

		for _, tagID := range tagIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO post_tags (post_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				p.ID, tagID,
			); err != nil {
				return fmt.Errorf("postgres: attach tag: %w", err)
			}
		}

		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete soft-deletes the post.
func (r *PostRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE posts SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete post: %w", err)
	}
	return nil
}

// List returns up to limit top-level posts ordered by id DESC, cursor
// used as an exclusive upper bound. nextCursor == "" signals no more
// rows.
func (r *PostRepository) List(ctx context.Context, userID *string, cursor *string, limit int) ([]*domain.Post, string, error) {
	if limit <= 0 {
		return nil, "", nil
	}

	// Use `limit + 1` to detect whether more rows remain without a second query.
	args := []any{limit + 1}
	where := []string{"deleted_at IS NULL", "parent_post_id IS NULL"}
	if userID != nil {
		args = append(args, *userID)
		where = append(where, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if cursor != nil && *cursor != "" {
		args = append(args, *cursor)
		where = append(where, fmt.Sprintf("id < $%d", len(args)))
	}

	rows, err := r.pool.Query(ctx, buildPostListQuery(where), args...)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list posts: %w", err)
	}
	defer rows.Close()
	return scanPostPage(rows, limit)
}

// ListByUserIDs returns the union of top-level posts authored by any
// sub in userIDs.
func (r *PostRepository) ListByUserIDs(ctx context.Context, userIDs []string, cursor *string, limit int) ([]*domain.Post, string, error) {
	if limit <= 0 || len(userIDs) == 0 {
		return nil, "", nil
	}
	args := []any{limit + 1, userIDs}
	where := []string{"deleted_at IS NULL", "parent_post_id IS NULL", "user_id = ANY($2)"}
	if cursor != nil && *cursor != "" {
		args = append(args, *cursor)
		where = append(where, fmt.Sprintf("id < $%d", len(args)))
	}

	rows, err := r.pool.Query(ctx, buildPostListQuery(where), args...)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list posts by user ids: %w", err)
	}
	defer rows.Close()
	return scanPostPage(rows, limit)
}

// ListReplies returns direct replies of postID.
func (r *PostRepository) ListReplies(ctx context.Context, postID string, cursor *string, limit int) ([]*domain.Post, string, error) {
	if limit <= 0 {
		return nil, "", nil
	}
	args := []any{limit + 1, postID}
	where := []string{"deleted_at IS NULL", "parent_post_id = $2"}
	if cursor != nil && *cursor != "" {
		args = append(args, *cursor)
		where = append(where, fmt.Sprintf("id < $%d", len(args)))
	}

	rows, err := r.pool.Query(ctx, buildPostListQuery(where), args...)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list replies: %w", err)
	}
	defer rows.Close()
	return scanPostPage(rows, limit)
}

// IncrementRepliesCount bumps the counter. No-op for missing / deleted rows.
func (r *PostRepository) IncrementRepliesCount(ctx context.Context, postID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE posts SET replies_count = replies_count + 1, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, postID)
	if err != nil {
		return fmt.Errorf("postgres: inc replies_count: %w", err)
	}
	return nil
}

// DecrementRepliesCount decrements the counter (floor 0).
func (r *PostRepository) DecrementRepliesCount(ctx context.Context, postID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE posts SET replies_count = GREATEST(replies_count - 1, 0), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, postID)
	if err != nil {
		return fmt.Errorf("postgres: dec replies_count: %w", err)
	}
	return nil
}

// IncrementLikesCount bumps the counter. No-op for missing / deleted rows.
func (r *PostRepository) IncrementLikesCount(ctx context.Context, postID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE posts SET likes_count = likes_count + 1, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, postID)
	if err != nil {
		return fmt.Errorf("postgres: inc likes_count: %w", err)
	}
	return nil
}

// DecrementLikesCount decrements the counter (floor 0).
func (r *PostRepository) DecrementLikesCount(ctx context.Context, postID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE posts SET likes_count = GREATEST(likes_count - 1, 0), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, postID)
	if err != nil {
		return fmt.Errorf("postgres: dec likes_count: %w", err)
	}
	return nil
}

// AttachOGP links urlHash to postID at position. Idempotent: both the
// (post_id, url_hash) primary key and the (post_id, position) unique
// index collapse to a no-op via ON CONFLICT DO NOTHING. First writer
// wins on position conflicts.
func (r *PostRepository) AttachOGP(ctx context.Context, postID, urlHash string, position int) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO post_ogp (post_id, url_hash, position)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, postID, urlHash, position)
	if err != nil {
		return fmt.Errorf("postgres: attach ogp: %w", err)
	}
	return nil
}

// buildPostListQuery assembles the cursor-aware SELECT used by every
// list endpoint. All three callers share the same columns, table,
// order, and limit-placeholder convention ($1 always binds limit+1);
// only the WHERE clause varies.
func buildPostListQuery(where []string) string {
	return "SELECT " + postSelectColumns +
		" FROM posts WHERE " + strings.Join(where, " AND ") +
		" ORDER BY id DESC LIMIT $1"
}

// scanPostPage consumes at most limit+1 rows and returns (page,
// nextCursor). When a limit+1th row is present, the caller's
// deferred rows.Close() releases it; we stop appending to avoid
// returning it.
func scanPostPage(rows pgx.Rows, limit int) ([]*domain.Post, string, error) {
	posts := make([]*domain.Post, 0, limit)
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, "", fmt.Errorf("postgres: scan post: %w", err)
		}
		posts = append(posts, p)
		if len(posts) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("postgres: list posts iterate: %w", err)
	}
	if len(posts) > limit {
		page := posts[:limit]
		return page, page[limit-1].ID, nil
	}
	return posts, "", nil
}
