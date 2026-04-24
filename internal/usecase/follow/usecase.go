// Package follow contains follow / unfollow / list use cases.
package follow

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
)

// DefaultPageLimit is the fallback page size for ListFollowers /
// ListFollowing.
const DefaultPageLimit = 20

// MaxPageLimit caps the per-request page size.
const MaxPageLimit = 50

// Result is the response payload for Follow / Unfollow. It returns
// the target's fresh follower count so the client can update the profile
// UI without a second roundtrip.
type Result struct {
	Following      bool
	FollowersCount int64
}

// UseCase creates a directed follow relation from followerSub to
// followeeSub. Idempotent.
type UseCase struct {
	userRepo   repository.UserRepository
	followRepo repository.FollowRepository
}

// NewUseCase constructs a follow UseCase.
func NewUseCase(userRepo repository.UserRepository, followRepo repository.FollowRepository) *UseCase {
	return &UseCase{userRepo: userRepo, followRepo: followRepo}
}

// Execute performs the follow. Repeat calls return the existing state
// without bumping the counters a second time.
func (uc *UseCase) Execute(ctx context.Context, followerSub, followeeSub string) (*Result, error) {
	if followerSub == "" {
		return nil, errors.Unauthorized("authentication required")
	}
	f := &domain.Follow{FollowerSub: followerSub, FolloweeSub: followeeSub}
	if err := f.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}
	target, err := uc.userRepo.GetBySub(ctx, followeeSub)
	if err != nil {
		return nil, errors.DatabaseError("failed to load target user", err)
	}
	if target == nil || target.DeletedAt != nil {
		return nil, errors.NotFound("user not found")
	}

	inserted, err := uc.followRepo.Create(ctx, followerSub, followeeSub)
	if err != nil {
		return nil, errors.DatabaseError("failed to create follow", err)
	}
	// Use the already-loaded target to avoid a second GetBySub. The
	// counter mutation races with other writers in a concurrent system,
	// but the staleness is bounded to the transaction boundary.
	followersCount := target.FollowersCount
	if inserted {
		if err := uc.userRepo.IncrementFollowingCount(ctx, followerSub); err != nil {
			return nil, errors.DatabaseError("failed to bump following_count", err)
		}
		if err := uc.userRepo.IncrementFollowersCount(ctx, followeeSub); err != nil {
			return nil, errors.DatabaseError("failed to bump followers_count", err)
		}
		followersCount++
	}
	return &Result{Following: true, FollowersCount: followersCount}, nil
}

// UnfollowUseCase removes a directed follow relation. Idempotent.
type UnfollowUseCase struct {
	userRepo   repository.UserRepository
	followRepo repository.FollowRepository
}

// NewUnfollowUseCase constructs an UnfollowUseCase.
func NewUnfollowUseCase(userRepo repository.UserRepository, followRepo repository.FollowRepository) *UnfollowUseCase {
	return &UnfollowUseCase{userRepo: userRepo, followRepo: followRepo}
}

// Execute performs the unfollow. Repeat calls are no-ops.
func (uc *UnfollowUseCase) Execute(ctx context.Context, followerSub, followeeSub string) (*Result, error) {
	if followerSub == "" {
		return nil, errors.Unauthorized("authentication required")
	}
	if followerSub == followeeSub {
		return nil, errors.ValidationFailed("cannot unfollow yourself")
	}
	if followeeSub == "" {
		return nil, errors.InvalidRequest("target sub is required", nil)
	}

	deleted, err := uc.followRepo.Delete(ctx, followerSub, followeeSub)
	if err != nil {
		return nil, errors.DatabaseError("failed to delete follow", err)
	}
	if deleted {
		if err := uc.userRepo.DecrementFollowingCount(ctx, followerSub); err != nil {
			return nil, errors.DatabaseError("failed to decrement following_count", err)
		}
		if err := uc.userRepo.DecrementFollowersCount(ctx, followeeSub); err != nil {
			return nil, errors.DatabaseError("failed to decrement followers_count", err)
		}
	}

	refreshed, err := uc.userRepo.GetBySub(ctx, followeeSub)
	if err != nil {
		return nil, errors.DatabaseError("failed to reload target user", err)
	}
	return &Result{Following: false, FollowersCount: countOrZero(refreshed)}, nil
}

// ListFollowersUseCase returns a paginated list of users that follow sub.
type ListFollowersUseCase struct {
	userRepo   repository.UserRepository
	followRepo repository.FollowRepository
}

// NewListFollowersUseCase constructs a ListFollowersUseCase.
func NewListFollowersUseCase(userRepo repository.UserRepository, followRepo repository.FollowRepository) *ListFollowersUseCase {
	return &ListFollowersUseCase{userRepo: userRepo, followRepo: followRepo}
}

// Execute returns one page of (User, nextCursor). The returned slice
// preserves the repository's (created_at DESC, peer DESC) ordering.
func (uc *ListFollowersUseCase) Execute(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.User, string, error) {
	if sub == "" {
		return nil, "", errors.InvalidRequest("sub is required", nil)
	}
	limit = normalizeLimit(limit)
	rows, next, err := uc.followRepo.ListFollowers(ctx, sub, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list followers", err)
	}
	peers := make([]string, len(rows))
	for i, f := range rows {
		peers[i] = f.FollowerSub
	}
	users, err := uc.userRepo.ListBySubs(ctx, peers)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to load follower users", err)
	}
	return orderedUsers(peers, users), next, nil
}

// ListFollowingUseCase returns a paginated list of users that sub
// follows.
type ListFollowingUseCase struct {
	userRepo   repository.UserRepository
	followRepo repository.FollowRepository
}

// NewListFollowingUseCase constructs a ListFollowingUseCase.
func NewListFollowingUseCase(userRepo repository.UserRepository, followRepo repository.FollowRepository) *ListFollowingUseCase {
	return &ListFollowingUseCase{userRepo: userRepo, followRepo: followRepo}
}

// Execute returns one page of (User, nextCursor).
func (uc *ListFollowingUseCase) Execute(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.User, string, error) {
	if sub == "" {
		return nil, "", errors.InvalidRequest("sub is required", nil)
	}
	limit = normalizeLimit(limit)
	rows, next, err := uc.followRepo.ListFollowing(ctx, sub, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list following", err)
	}
	peers := make([]string, len(rows))
	for i, f := range rows {
		peers[i] = f.FolloweeSub
	}
	users, err := uc.userRepo.ListBySubs(ctx, peers)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to load followed users", err)
	}
	return orderedUsers(peers, users), next, nil
}

// orderedUsers aligns users from the ListBySubs map back into the original
// peers-slice order (so the pagination order survives the map lookup).
// Unknown subs are skipped — a missing user would mean a dangling follow
// row, which shouldn't happen under ON DELETE CASCADE.
func orderedUsers(peers []string, users map[string]*domain.User) []*domain.User {
	out := make([]*domain.User, 0, len(peers))
	for _, p := range peers {
		if u, ok := users[p]; ok {
			out = append(out, u)
		}
	}
	return out
}

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > MaxPageLimit {
		return DefaultPageLimit
	}
	return limit
}

func countOrZero(u *domain.User) int64 {
	if u == nil {
		return 0
	}
	return u.FollowersCount
}
