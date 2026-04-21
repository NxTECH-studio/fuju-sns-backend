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

// BadgeRepository is the postgres-backed repository.BadgeRepository.
// Single struct handles both the badges master table (migration 005)
// and the user_badges join (grant / revoke / per-user lookup) so that
// the repository contract in repository.go maps 1:1 to SQL.
type BadgeRepository struct {
	pool *pgxpool.Pool
}

// NewBadgeRepository constructs a BadgeRepository bound to pool.
func NewBadgeRepository(pool *pgxpool.Pool) *BadgeRepository {
	return &BadgeRepository{pool: pool}
}

const badgeSelectColumns = `id, key, label, description, icon_url, color,
	priority, created_at, updated_at`

func scanBadge(row pgx.Row) (*domain.Badge, error) {
	var b domain.Badge
	err := row.Scan(&b.ID, &b.Key, &b.Label, &b.Description, &b.IconURL, &b.Color,
		&b.Priority, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListAll returns every badge master row ordered by priority asc,
// tie-broken by key asc (matches inmemory's sortBadgesByPriority).
func (r *BadgeRepository) ListAll(ctx context.Context) ([]*domain.Badge, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+badgeSelectColumns+`
		FROM badges
		ORDER BY priority ASC, key ASC`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list badges: %w", err)
	}
	defer rows.Close()

	var out []*domain.Badge
	for rows.Next() {
		b, err := scanBadge(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan badge: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list badges iterate: %w", err)
	}
	return out, nil
}

// GetByKey returns (nil, nil) on miss.
func (r *BadgeRepository) GetByKey(ctx context.Context, key string) (*domain.Badge, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+badgeSelectColumns+` FROM badges WHERE key = $1`, key)
	b, err := scanBadge(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get badge by key: %w", err)
	}
	return b, nil
}

// GetByID returns (nil, nil) on miss.
func (r *BadgeRepository) GetByID(ctx context.Context, id string) (*domain.Badge, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+badgeSelectColumns+` FROM badges WHERE id = $1`, id)
	b, err := scanBadge(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: get badge by id: %w", err)
	}
	return b, nil
}

// Create inserts a new badge master row. Returns (nil, nil) on a
// duplicate key — matches inmemory's "first writer wins" contract so
// the admin handler can detect the collision without a separate
// existence check.
func (r *BadgeRepository) Create(ctx context.Context, badge *domain.Badge) (*domain.Badge, error) {
	createdAt := badge.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO badges (id, key, label, description, icon_url, color, priority,
		                   created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (key) DO NOTHING
		RETURNING `+badgeSelectColumns,
		badge.ID, badge.Key, badge.Label, badge.Description, badge.IconURL,
		badge.Color, badge.Priority, createdAt)
	b, err := scanBadge(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Duplicate key → ON CONFLICT DO NOTHING produced no row.
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: create badge: %w", err)
	}
	return b, nil
}

// Update mutates the caller-editable fields (label / description /
// icon_url / color / priority); key and id are frozen. Returns
// (nil, nil) when the id is unknown, matching the inmemory contract.
func (r *BadgeRepository) Update(ctx context.Context, badge *domain.Badge) (*domain.Badge, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE badges SET
			label       = $2,
			description = $3,
			icon_url    = $4,
			color       = $5,
			priority    = $6,
			updated_at  = NOW()
		WHERE id = $1
		RETURNING `+badgeSelectColumns,
		badge.ID, badge.Label, badge.Description, badge.IconURL,
		badge.Color, badge.Priority)
	b, err := scanBadge(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: update badge: %w", err)
	}
	return b, nil
}

// Grant upserts a user_badges row. Regrants overwrite expires_at /
// reason (mirrors inmemory's "always overwrite" semantics) so that an
// admin can renew a grant by re-issuing with a fresh expiry.
func (r *BadgeRepository) Grant(ctx context.Context, userID, badgeID, grantedBy string, expiresAt *time.Time, reason string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_badges (user_id, badge_id, granted_at, granted_by, expires_at, reason)
		VALUES ($1, $2, NOW(), $3, $4, $5)
		ON CONFLICT (user_id, badge_id) DO UPDATE SET
			granted_at = NOW(),
			granted_by = EXCLUDED.granted_by,
			expires_at = EXCLUDED.expires_at,
			reason     = EXCLUDED.reason`,
		userID, badgeID, grantedBy, expiresAt, reason)
	if err != nil {
		return fmt.Errorf("postgres: grant badge: %w", err)
	}
	return nil
}

// Revoke deletes a grant. Revoking a missing row is a no-op.
func (r *BadgeRepository) Revoke(ctx context.Context, userID, badgeID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_badges WHERE user_id = $1 AND badge_id = $2`,
		userID, badgeID)
	if err != nil {
		return fmt.Errorf("postgres: revoke badge: %w", err)
	}
	return nil
}

// badgeJoinColumns is the same columns as badgeSelectColumns, but
// prefixed with the "b." alias used by user_badges JOIN queries.
const badgeJoinColumns = `b.id, b.key, b.label, b.description, b.icon_url, b.color,
	b.priority, b.created_at, b.updated_at`

// ListByUserID returns the user's currently-active badges, ordered by
// priority asc tie-broken by key. Expired grants (expires_at past now)
// are filtered.
func (r *BadgeRepository) ListByUserID(ctx context.Context, userID string) ([]*domain.Badge, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+badgeJoinColumns+`
		FROM badges b
		JOIN user_badges ub ON ub.badge_id = b.id
		WHERE ub.user_id = $1
		  AND (ub.expires_at IS NULL OR ub.expires_at > NOW())
		ORDER BY b.priority ASC, b.key ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list badges by user: %w", err)
	}
	defer rows.Close()

	var out []*domain.Badge
	for rows.Next() {
		b, err := scanBadge(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan user badge: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list badges by user iterate: %w", err)
	}
	return out, nil
}

// ListByUserIDs batches ListByUserID across many users. Users with no
// active grants are absent from the returned map.
func (r *BadgeRepository) ListByUserIDs(ctx context.Context, userIDs []string) (map[string][]*domain.Badge, error) {
	out := make(map[string][]*domain.Badge, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT ub.user_id, `+badgeJoinColumns+`
		FROM badges b
		JOIN user_badges ub ON ub.badge_id = b.id
		WHERE ub.user_id = ANY($1)
		  AND (ub.expires_at IS NULL OR ub.expires_at > NOW())
		ORDER BY b.priority ASC, b.key ASC`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: list badges by users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var userID string
		var b domain.Badge
		if err := rows.Scan(&userID, &b.ID, &b.Key, &b.Label, &b.Description,
			&b.IconURL, &b.Color, &b.Priority, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan user badge: %w", err)
		}
		cp := b
		out[userID] = append(out[userID], &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list badges by users iterate: %w", err)
	}
	return out, nil
}
