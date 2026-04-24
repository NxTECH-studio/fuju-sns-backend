package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository is the postgres-backed implementation of
// repository.UserRepository. Row shape matches migration 001 plus the
// follow counters added in 006.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository constructs a UserRepository bound to pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

const userSelectColumns = `sub, display_name_cached, display_id_cached, icon_url_cached,
	profile_refreshed_at, bio, banner_url, is_admin,
	followers_count, following_count,
	created_at, updated_at, deleted_at`

// scanUser maps a single row (ordered as userSelectColumns) into a
// domain.User. The helper is shared by every read path so column
// ordering stays in lock-step with the SELECT clause.
func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	err := row.Scan(
		&u.Sub,
		&u.DisplayNameCached,
		&u.DisplayIDCached,
		&u.IconURLCached,
		&u.ProfileRefreshedAt,
		&u.Bio,
		&u.BannerURL,
		&u.IsAdmin,
		&u.FollowersCount,
		&u.FollowingCount,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetBySub returns (nil, nil) on miss. Matches the in-memory contract
// of surfacing soft-deleted rows as-is; the handler layer filters.
func (r *UserRepository) GetBySub(ctx context.Context, sub string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userSelectColumns+` FROM users WHERE sub = $1`, sub)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get user by sub: %w", err)
	}
	return u, nil
}

// Upsert mirrors the in-memory contract:
//   - insert-path writes every caller-supplied field
//   - update-path only refreshes cached profile + profile_refreshed_at,
//     never touches is_admin / created_at / followers_count /
//     following_count / deleted_at, and preserves bio / banner_url when
//     the caller sent the zero value
//   - updated_at is always bumped to NOW()
func (r *UserRepository) Upsert(ctx context.Context, user *domain.User) (*domain.User, error) {
	createdAt := user.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	const q = `
	INSERT INTO users (
		sub, display_name_cached, display_id_cached, icon_url_cached,
		profile_refreshed_at, bio, banner_url, is_admin,
		followers_count, following_count,
		created_at, updated_at
	)
	VALUES (
		$1, $2, $3, $4,
		$5, $6, $7, $8,
		$9, $10,
		$11, NOW()
	)
	ON CONFLICT (sub) DO UPDATE SET
		display_name_cached = EXCLUDED.display_name_cached,
		display_id_cached   = EXCLUDED.display_id_cached,
		icon_url_cached     = EXCLUDED.icon_url_cached,
		profile_refreshed_at = EXCLUDED.profile_refreshed_at,
		bio        = CASE WHEN EXCLUDED.bio = ''        THEN users.bio        ELSE EXCLUDED.bio        END,
		banner_url = CASE WHEN EXCLUDED.banner_url = '' THEN users.banner_url ELSE EXCLUDED.banner_url END,
		updated_at = NOW()
	RETURNING ` + userSelectColumns
	row := r.pool.QueryRow(ctx, q,
		user.Sub,
		user.DisplayNameCached,
		user.DisplayIDCached,
		user.IconURLCached,
		user.ProfileRefreshedAt,
		user.Bio,
		user.BannerURL,
		user.IsAdmin,
		user.FollowersCount,
		user.FollowingCount,
		createdAt,
	)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("postgres: upsert user: %w", err)
	}
	return u, nil
}

// UpdateProfile writes only the SNS-owned profile fields (bio,
// banner_url). Returns (nil, nil) when the target row does not exist,
// matching the in-memory behaviour.
func (r *UserRepository) UpdateProfile(ctx context.Context, sub string, req *domain.UpdateUserProfileRequest) (*domain.User, error) {
	const q = `
	UPDATE users SET
		bio        = COALESCE($2, bio),
		banner_url = COALESCE($3, banner_url),
		updated_at = NOW()
	WHERE sub = $1
	RETURNING ` + userSelectColumns
	row := r.pool.QueryRow(ctx, q, sub, req.Bio, req.BannerURL)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: update user profile: %w", err)
	}
	return u, nil
}

// List returns a page of live users (deleted_at IS NULL) plus the
// total row count for pagination.
func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]*domain.User, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count users: %w", err)
	}
	if limit <= 0 || total == 0 {
		return nil, total, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+userSelectColumns+`
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC, sub DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()

	var out []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("postgres: scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: list users iterate: %w", err)
	}
	return out, total, nil
}

// ListBySubs returns live users keyed by sub. Missing and soft-deleted
// subs are absent from the map.
func (r *UserRepository) ListBySubs(ctx context.Context, subs []string) (map[string]*domain.User, error) {
	out := make(map[string]*domain.User, len(subs))
	if len(subs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+userSelectColumns+`
		FROM users
		WHERE sub = ANY($1) AND deleted_at IS NULL`, subs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users by subs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan user: %w", err)
		}
		out[u.Sub] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list users by subs iterate: %w", err)
	}
	return out, nil
}

// IncrementFollowersCount bumps the denormalized counter.
func (r *UserRepository) IncrementFollowersCount(ctx context.Context, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET followers_count = followers_count + 1, updated_at = NOW() WHERE sub = $1`, sub)
	if err != nil {
		return fmt.Errorf("postgres: inc followers_count: %w", err)
	}
	return nil
}

// DecrementFollowersCount decrements the counter, flooring at 0.
func (r *UserRepository) DecrementFollowersCount(ctx context.Context, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET followers_count = GREATEST(followers_count - 1, 0), updated_at = NOW() WHERE sub = $1`, sub)
	if err != nil {
		return fmt.Errorf("postgres: dec followers_count: %w", err)
	}
	return nil
}

// IncrementFollowingCount bumps the denormalized counter.
func (r *UserRepository) IncrementFollowingCount(ctx context.Context, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET following_count = following_count + 1, updated_at = NOW() WHERE sub = $1`, sub)
	if err != nil {
		return fmt.Errorf("postgres: inc following_count: %w", err)
	}
	return nil
}

// DecrementFollowingCount decrements the counter, flooring at 0.
func (r *UserRepository) DecrementFollowingCount(ctx context.Context, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET following_count = GREATEST(following_count - 1, 0), updated_at = NOW() WHERE sub = $1`, sub)
	if err != nil {
		return fmt.Errorf("postgres: dec following_count: %w", err)
	}
	return nil
}

// Delete soft-deletes the user.
func (r *UserRepository) Delete(ctx context.Context, sub string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET deleted_at = NOW(), updated_at = NOW() WHERE sub = $1 AND deleted_at IS NULL`, sub)
	if err != nil {
		return fmt.Errorf("postgres: delete user: %w", err)
	}
	return nil
}
