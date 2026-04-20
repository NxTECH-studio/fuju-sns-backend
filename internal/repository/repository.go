// Package repository defines repository interfaces.
package repository

import (
	"context"
	"time"

	"github.com/fuju/backend/internal/domain"
)

// UserRepository defines user persistence operations. Users are keyed by
// AuthCore's sub (ULID).
type UserRepository interface {
	// GetBySub retrieves a user by AuthCore sub. Returns (nil, nil) when not found.
	GetBySub(ctx context.Context, sub string) (*domain.User, error)

	// Upsert inserts or updates a user. Used by the hydrate flow. Existing
	// rows must preserve is_admin — callers that want to refresh cached
	// profile fields should leave User.IsAdmin as zero and rely on the
	// repository to keep the stored value.
	Upsert(ctx context.Context, user *domain.User) (*domain.User, error)

	// UpdateProfile updates SNS-owned profile fields (bio, banner_url).
	UpdateProfile(ctx context.Context, sub string, req *domain.UpdateUserProfileRequest) (*domain.User, error)

	// List retrieves a paginated list of users.
	List(ctx context.Context, limit, offset int) ([]*domain.User, int, error)

	// Delete soft-deletes a user.
	Delete(ctx context.Context, sub string) error
}

// PostRepository defines post persistence operations.
type PostRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Post, error)
	List(ctx context.Context, userID *string, limit, offset int) ([]*domain.Post, int, error)
	Create(ctx context.Context, post *domain.Post) (*domain.Post, error)
	Delete(ctx context.Context, id string) error
	IncrementCommentCount(ctx context.Context, postID string) error
	DecrementCommentCount(ctx context.Context, postID string) error
}

// CommentRepository defines comment persistence operations.
type CommentRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Comment, error)
	ListByPostID(ctx context.Context, postID string, limit, offset int) ([]*domain.Comment, int, error)
	Create(ctx context.Context, comment *domain.Comment) (*domain.Comment, error)
	Delete(ctx context.Context, id string) error
}

// ImageRepository defines image persistence operations.
type ImageRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Image, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.Image, error)
	Create(ctx context.Context, image *domain.Image) (*domain.Image, error)
	Delete(ctx context.Context, id string) error
}

// BadgeRepository defines badge master + user_badges persistence operations.
// ListByUserID / ListByUserIDs return only currently-active grants
// (expires_at IS NULL OR expires_at > NOW()).
type BadgeRepository interface {
	// Master table.
	ListAll(ctx context.Context) ([]*domain.Badge, error)
	GetByKey(ctx context.Context, key string) (*domain.Badge, error)
	GetByID(ctx context.Context, id string) (*domain.Badge, error)
	Create(ctx context.Context, badge *domain.Badge) (*domain.Badge, error)
	Update(ctx context.Context, badge *domain.Badge) (*domain.Badge, error)

	// User grants.
	Grant(ctx context.Context, userID, badgeID, grantedBy string, expiresAt *time.Time, reason string) error
	Revoke(ctx context.Context, userID, badgeID string) error
	ListByUserID(ctx context.Context, userID string) ([]*domain.Badge, error)

	// N+1 avoidance for list endpoints: one call resolves many users.
	ListByUserIDs(ctx context.Context, userIDs []string) (map[string][]*domain.Badge, error)
}
