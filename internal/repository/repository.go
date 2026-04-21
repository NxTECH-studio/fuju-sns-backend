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

	// ListBySubs returns a map keyed by sub for batched author lookups
	// (timeline / post hydration). Missing subs are absent from the map.
	ListBySubs(ctx context.Context, subs []string) (map[string]*domain.User, error)

	// Follow counter mutators. The follow usecase calls these exactly
	// once per (idempotent) state transition.
	IncrementFollowersCount(ctx context.Context, sub string) error
	DecrementFollowersCount(ctx context.Context, sub string) error
	IncrementFollowingCount(ctx context.Context, sub string) error
	DecrementFollowingCount(ctx context.Context, sub string) error

	// Delete soft-deletes a user.
	Delete(ctx context.Context, sub string) error
}

// FollowRepository defines persistence for directed follow relations.
// Create and Delete are idempotent: the boolean return signals whether the
// row state actually changed so the caller only adjusts the denormalized
// counters on the 0↔1 transition.
//
// List cursors use the composite-key format
//
//	base64url(RFC3339Nano(created_at) + "|" + peer_sub)
//
// where peer_sub is follower_sub for ListFollowers and followee_sub for
// ListFollowing. Ordering is (created_at DESC, peer_sub DESC). Rows strictly
// less than the cursor are returned. An empty returned nextCursor means no
// more results.
type FollowRepository interface {
	Create(ctx context.Context, followerSub, followeeSub string) (bool, error)
	Delete(ctx context.Context, followerSub, followeeSub string) (bool, error)
	IsFollowing(ctx context.Context, followerSub, followeeSub string) (bool, error)

	// ListFollowingSubs returns every sub that followerSub follows. Used
	// to compute the home timeline author set in a single fan-out.
	ListFollowingSubs(ctx context.Context, followerSub string) ([]string, error)

	ListFollowers(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.Follow, string, error)
	ListFollowing(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.Follow, string, error)

	// AreFollowing batches the "does the viewer follow each target?"
	// question for post hydration. Unfollowed targets are absent from the
	// returned map.
	AreFollowing(ctx context.Context, viewerSub string, targetSubs []string) (map[string]bool, error)
}

// PostRepository defines post persistence operations. All list methods use
// cursor-based pagination keyed on the ULID primary key (which is
// lexicographically sorted by time). `cursor == nil` means "start from the
// newest". An empty returned `nextCursor` means "no more results".
type PostRepository interface {
	// GetByID returns (nil, nil) for missing OR soft-deleted posts.
	GetByID(ctx context.Context, id string) (*domain.Post, error)

	// Create inserts the post plus its post_images and post_tags join rows
	// in a single logical transaction. imageIDs ordering is preserved as
	// post_images.position (0..N-1). tagIDs have no defined ordering.
	Create(ctx context.Context, post *domain.Post, imageIDs []string, tagIDs []string) (*domain.Post, error)

	// Delete soft-deletes the post.
	Delete(ctx context.Context, id string) error

	// List returns posts ordered by id DESC. Pass userID != nil to scope to
	// a single author.
	List(ctx context.Context, userID *string, cursor *string, limit int) ([]*domain.Post, string, error)

	// ListByUserIDs returns posts authored by any sub in userIDs, ordered
	// by id DESC. Used by the follow timeline in a later task.
	ListByUserIDs(ctx context.Context, userIDs []string, cursor *string, limit int) ([]*domain.Post, string, error)

	// ListReplies returns direct replies (parent_post_id == postID).
	ListReplies(ctx context.Context, postID string, cursor *string, limit int) ([]*domain.Post, string, error)

	IncrementRepliesCount(ctx context.Context, postID string) error
	DecrementRepliesCount(ctx context.Context, postID string) error
	IncrementLikesCount(ctx context.Context, postID string) error
	DecrementLikesCount(ctx context.Context, postID string) error
}

// LikeRepository defines like persistence operations. Create and Delete
// are idempotent: the boolean return tells the app whether the row state
// actually changed so it can adjust the denormalized likes counter at
// most once.
type LikeRepository interface {
	Create(ctx context.Context, userID, postID string) (bool, error)
	Delete(ctx context.Context, userID, postID string) (bool, error)
	IsLikedBy(ctx context.Context, userID, postID string) (bool, error)
	// ListLikedPostIDsByUser returns a map postID -> true for each post in
	// postIDs the user has liked. Unliked posts are absent from the map.
	ListLikedPostIDsByUser(ctx context.Context, userID string, postIDs []string) (map[string]bool, error)
}

// TagRepository defines tag persistence operations.
type TagRepository interface {
	// UpsertByNames inserts any name not yet present and returns the full
	// set of Tag rows for the given names. Names must already be
	// normalized (lower-cased, trimmed) by the caller.
	UpsertByNames(ctx context.Context, names []string) ([]*domain.Tag, error)
	ListByPostID(ctx context.Context, postID string) ([]*domain.Tag, error)
	// ListByPostIDs batches ListByPostID across posts. Posts with no tags
	// are absent from the returned map.
	ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.Tag, error)
}

// ImageRepository defines image persistence operations.
type ImageRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Image, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.Image, error)
	Create(ctx context.Context, image *domain.Image) (*domain.Image, error)
	Delete(ctx context.Context, id string) error
	// ListByPostID returns the images attached to a post ordered by
	// post_images.position ascending.
	ListByPostID(ctx context.Context, postID string) ([]*domain.Image, error)
	// ListByPostIDs batches ListByPostID. Posts with no images are absent
	// from the returned map.
	ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.Image, error)
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
