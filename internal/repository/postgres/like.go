package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LikeRepository is the postgres-backed repository.LikeRepository.
type LikeRepository struct {
	pool *pgxpool.Pool
}

// NewLikeRepository constructs a LikeRepository bound to pool.
func NewLikeRepository(pool *pgxpool.Pool) *LikeRepository {
	return &LikeRepository{pool: pool}
}

// Create inserts a like row idempotently. Returns true iff a new row
// was actually created — ON CONFLICT DO NOTHING produces a zero
// RowsAffected when the row already existed.
func (r *LikeRepository) Create(ctx context.Context, userID, postID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO likes (user_id, post_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id, post_id) DO NOTHING`,
		userID, postID)
	if err != nil {
		return false, fmt.Errorf("postgres: create like: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Delete removes a like row idempotently. Returns true iff a row was
// actually removed.
func (r *LikeRepository) Delete(ctx context.Context, userID, postID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`,
		userID, postID)
	if err != nil {
		return false, fmt.Errorf("postgres: delete like: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// IsLikedBy reports whether (userID, postID) exists.
func (r *LikeRepository) IsLikedBy(ctx context.Context, userID, postID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM likes WHERE user_id = $1 AND post_id = $2)`,
		userID, postID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("postgres: is liked by: %w", err)
	}
	return exists, nil
}

// ListLikedPostIDsByUser returns a map postID -> true for each post
// the user has liked, restricted to the given postIDs.
func (r *LikeRepository) ListLikedPostIDsByUser(ctx context.Context, userID string, postIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT post_id FROM likes
		WHERE user_id = $1 AND post_id = ANY($2)`, userID, postIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list liked post ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: scan like: %w", err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list liked post ids iterate: %w", err)
	}
	return out, nil
}
