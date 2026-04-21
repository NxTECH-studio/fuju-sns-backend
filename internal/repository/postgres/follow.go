package postgres

import (
	"context"
	"fmt"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/sharedcursor"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FollowRepository is the postgres-backed repository.FollowRepository.
// Schema lives in migration 006 (follows table + idx_follows_*).
type FollowRepository struct {
	pool *pgxpool.Pool
}

// NewFollowRepository constructs a FollowRepository bound to pool.
func NewFollowRepository(pool *pgxpool.Pool) *FollowRepository {
	return &FollowRepository{pool: pool}
}

// Create inserts a follow row idempotently. Returns (true, nil) iff a
// new row was actually added; duplicate pairs collapse to (false, nil)
// via ON CONFLICT DO NOTHING.
func (r *FollowRepository) Create(ctx context.Context, followerSub, followeeSub string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO follows (follower_sub, followee_sub, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (follower_sub, followee_sub) DO NOTHING`,
		followerSub, followeeSub)
	if err != nil {
		return false, fmt.Errorf("postgres: create follow: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Delete removes a follow row idempotently. Returns (true, nil) iff a
// row was actually removed.
func (r *FollowRepository) Delete(ctx context.Context, followerSub, followeeSub string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM follows WHERE follower_sub = $1 AND followee_sub = $2`,
		followerSub, followeeSub)
	if err != nil {
		return false, fmt.Errorf("postgres: delete follow: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// IsFollowing reports whether followerSub follows followeeSub.
func (r *FollowRepository) IsFollowing(ctx context.Context, followerSub, followeeSub string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_sub = $1 AND followee_sub = $2)`,
		followerSub, followeeSub).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("postgres: is following: %w", err)
	}
	return exists, nil
}

// ListFollowingSubs returns every followee_sub that followerSub
// follows, in arbitrary order.
func (r *FollowRepository) ListFollowingSubs(ctx context.Context, followerSub string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT followee_sub FROM follows WHERE follower_sub = $1`, followerSub)
	if err != nil {
		return nil, fmt.Errorf("postgres: list following subs: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var sub string
		if err := rows.Scan(&sub); err != nil {
			return nil, fmt.Errorf("postgres: scan following sub: %w", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list following subs iterate: %w", err)
	}
	return out, nil
}

// ListFollowers returns the followers of sub, ordered by
// (created_at DESC, follower_sub DESC) with the composite cursor from
// sharedcursor.
func (r *FollowRepository) ListFollowers(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.Follow, string, error) {
	return r.listSide(ctx, sub, cursor, limit, true)
}

// ListFollowing returns the accounts sub follows, ordered by
// (created_at DESC, followee_sub DESC).
func (r *FollowRepository) ListFollowing(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.Follow, string, error) {
	return r.listSide(ctx, sub, cursor, limit, false)
}

// listSide is the shared driver for ListFollowers (followersSide=true,
// filter on followee_sub) and ListFollowing (false, filter on
// follower_sub). The composite cursor `(created_at, peer_sub)` means we
// return rows strictly "earlier" than the cursor under the ordering
// (created_at DESC, peer_sub DESC).
func (r *FollowRepository) listSide(ctx context.Context, sub string, cursor *string, limit int, followersSide bool) ([]*domain.Follow, string, error) {
	if limit <= 0 {
		return nil, "", nil
	}

	// DecodeFollow returns hasCursor=false for nil / empty / malformed
	// cursors. Treating all three as "start from the top" matches the
	// in-memory tolerance and spares the caller an extra error path.
	cursorTime, cursorPeer, hasCursor := sharedcursor.DecodeFollow(cursor)

	var filterCol, peerCol string
	if followersSide {
		filterCol, peerCol = "followee_sub", "follower_sub"
	} else {
		filterCol, peerCol = "follower_sub", "followee_sub"
	}

	args := []any{limit + 1, sub}
	where := filterCol + " = $2"
	if hasCursor {
		args = append(args, cursorTime, cursorPeer)
		// (created_at, peer) < (cursorTime, cursorPeer) in lexicographic
		// order → row sorts *after* the cursor under DESC/DESC, i.e.
		// the next page. The tuple comparison mirrors the composite
		// index key shape.
		where += " AND (created_at, " + peerCol + ") < ($3, $4)"
	}

	q := `SELECT follower_sub, followee_sub, created_at
	      FROM follows
	      WHERE ` + where + `
	      ORDER BY created_at DESC, ` + peerCol + ` DESC
	      LIMIT $1`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("postgres: list follows: %w", err)
	}
	defer rows.Close()

	follows := make([]*domain.Follow, 0, limit)
	for rows.Next() {
		var f domain.Follow
		if err := rows.Scan(&f.FollowerSub, &f.FolloweeSub, &f.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("postgres: scan follow: %w", err)
		}
		follows = append(follows, &f)
		if len(follows) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("postgres: list follows iterate: %w", err)
	}

	if len(follows) <= limit {
		return follows, "", nil
	}
	page := follows[:limit]
	last := page[limit-1]
	peer := last.FollowerSub
	if !followersSide {
		peer = last.FolloweeSub
	}
	return page, sharedcursor.EncodeFollow(last.CreatedAt, peer), nil
}

// AreFollowing returns which of targetSubs the viewer follows. Subs
// absent from the map are not followed; the caller gets back a map
// exactly sized to the hits.
func (r *FollowRepository) AreFollowing(ctx context.Context, viewerSub string, targetSubs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(targetSubs))
	if len(targetSubs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT followee_sub FROM follows
		WHERE follower_sub = $1 AND followee_sub = ANY($2)`,
		viewerSub, targetSubs)
	if err != nil {
		return nil, fmt.Errorf("postgres: are following: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sub string
		if err := rows.Scan(&sub); err != nil {
			return nil, fmt.Errorf("postgres: scan are-following: %w", err)
		}
		out[sub] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: are following iterate: %w", err)
	}
	return out, nil
}
