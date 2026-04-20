// Package user contains user business logic use cases.
package user

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/authcore"
	"github.com/fuju/backend/pkg/errors"
)

// DefaultProfileTTL controls how long a cached AuthCore profile is considered
// fresh before a refresh is attempted.
const DefaultProfileTTL = time.Hour

// GetUserUseCase fetches a user by sub. No hydrate. Intended for public
// reads of someone's profile; the caller should run the hydrate flow
// separately for the viewer.
type GetUserUseCase struct {
	userRepo repository.UserRepository
}

// NewGetUserUseCase creates a new GetUserUseCase.
func NewGetUserUseCase(userRepo repository.UserRepository) *GetUserUseCase {
	return &GetUserUseCase{userRepo: userRepo}
}

// Execute retrieves a user by sub.
func (uc *GetUserUseCase) Execute(ctx context.Context, sub string) (*domain.User, error) {
	if sub == "" {
		return nil, errors.InvalidRequest("sub is required", nil)
	}

	user, err := uc.userRepo.GetBySub(ctx, sub)
	if err != nil {
		return nil, errors.DatabaseError("failed to get user", err)
	}
	if user == nil {
		return nil, errors.NotFound("user not found")
	}

	return user, nil
}

// GetOrHydrateUserUseCase implements the lazy-create + TTL-refresh flow.
// For an authenticated sub we either look up the SNS mirror row and refresh
// its cached profile (when older than ttl), or we create the row the first
// time we see the sub.
//
// AuthCore outages on profile fetch are fail-open: the cached row is
// returned as-is and profile_refreshed_at is left stale so the next call
// retries.
type GetOrHydrateUserUseCase struct {
	userRepo repository.UserRepository
	authcore authcore.Client
	ttl      time.Duration
	now      func() time.Time
}

// NewGetOrHydrateUserUseCase constructs the use case. ttl==0 means the
// default TTL is used.
func NewGetOrHydrateUserUseCase(userRepo repository.UserRepository, client authcore.Client, ttl time.Duration) *GetOrHydrateUserUseCase {
	if ttl <= 0 {
		ttl = DefaultProfileTTL
	}
	return &GetOrHydrateUserUseCase{
		userRepo: userRepo,
		authcore: client,
		ttl:      ttl,
		now:      time.Now,
	}
}

// Execute returns the user row for sub, creating or refreshing it when
// needed.
func (uc *GetOrHydrateUserUseCase) Execute(ctx context.Context, sub string) (*domain.User, error) {
	if sub == "" {
		return nil, errors.InvalidRequest("sub is required", nil)
	}

	existing, err := uc.userRepo.GetBySub(ctx, sub)
	if err != nil {
		return nil, errors.DatabaseError("failed to get user", err)
	}

	now := uc.now()

	if existing == nil {
		profile, perr := uc.authcore.GetProfile(ctx, sub)
		user := &domain.User{Sub: sub, CreatedAt: now, UpdatedAt: now}
		if perr == nil {
			user.DisplayNameCached = profile.DisplayName
			user.DisplayIDCached = profile.DisplayID
			user.IconURLCached = profile.IconURL
			user.ProfileRefreshedAt = now
		}
		// If AuthCore is unreachable on first hydrate we still insert a
		// blank mirror row so we do not lose the authenticated sub; the
		// next request will retry hydration.

		saved, err := uc.userRepo.Upsert(ctx, user)
		if err != nil {
			return nil, errors.DatabaseError("failed to create user", err)
		}
		return saved, nil
	}

	if now.Sub(existing.ProfileRefreshedAt) < uc.ttl {
		return existing, nil
	}

	profile, perr := uc.authcore.GetProfile(ctx, sub)
	if perr != nil {
		// fail-open: return the stale mirror, retry next call
		if stderrors.Is(perr, authcore.ErrNotFound) {
			return nil, errors.NotFound("user not found in authcore")
		}
		return existing, nil
	}

	existing.DisplayNameCached = profile.DisplayName
	existing.DisplayIDCached = profile.DisplayID
	existing.IconURLCached = profile.IconURL
	existing.ProfileRefreshedAt = now

	saved, err := uc.userRepo.Upsert(ctx, existing)
	if err != nil {
		return nil, errors.DatabaseError("failed to refresh user", err)
	}
	return saved, nil
}

// UpdateUserProfileUseCase updates SNS-owned profile fields (bio, banner).
type UpdateUserProfileUseCase struct {
	userRepo repository.UserRepository
}

// NewUpdateUserProfileUseCase creates a new UpdateUserProfileUseCase.
func NewUpdateUserProfileUseCase(userRepo repository.UserRepository) *UpdateUserProfileUseCase {
	return &UpdateUserProfileUseCase{userRepo: userRepo}
}

// Execute updates the authenticated user's profile. Cross-user updates are
// rejected — the caller must pass their own sub as both targetSub and
// currentSub.
func (uc *UpdateUserProfileUseCase) Execute(ctx context.Context, targetSub, currentSub string, req *domain.UpdateUserProfileRequest) (*domain.User, error) {
	if targetSub != currentSub {
		return nil, errors.Forbidden("you can only update your own profile")
	}
	if req == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}

	// Validate the proposed bounds before writing.
	probe := &domain.User{}
	if req.Bio != nil {
		probe.Bio = *req.Bio
	}
	if req.BannerURL != nil {
		probe.BannerURL = *req.BannerURL
	}
	if err := probe.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	updated, err := uc.userRepo.UpdateProfile(ctx, targetSub, req)
	if err != nil {
		return nil, errors.DatabaseError("failed to update user profile", err)
	}
	if updated == nil {
		return nil, errors.NotFound("user not found")
	}
	return updated, nil
}

// ListUsersUseCase lists users with pagination.
type ListUsersUseCase struct {
	userRepo repository.UserRepository
}

// NewListUsersUseCase creates a new ListUsersUseCase.
func NewListUsersUseCase(userRepo repository.UserRepository) *ListUsersUseCase {
	return &ListUsersUseCase{userRepo: userRepo}
}

// Execute lists users with pagination.
func (uc *ListUsersUseCase) Execute(ctx context.Context, limit, offset int) ([]*domain.User, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	users, total, err := uc.userRepo.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, errors.DatabaseError("failed to list users", err)
	}

	return users, total, nil
}
