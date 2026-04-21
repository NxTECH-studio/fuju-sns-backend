// Package user contains user business logic use cases.
package user

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/authcore"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/logger"
)

// DefaultProfileTTL controls how long a cached AuthCore profile is considered
// fresh before a refresh is attempted.
const DefaultProfileTTL = time.Hour

// GetUserUseCase fetches a user by sub. No hydrate. Intended for public
// reads of someone's profile.
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
// The caller's AuthCore access token is read from context so AuthCore's
// profile endpoint (which is keyed off the caller's own token, not a sub
// path param) can be called on their behalf.
//
// AuthCore upstream failures on profile fetch are fail-open: the cached row
// is returned as-is and profile_refreshed_at is left stale so the next call
// retries. ErrNotFound from AuthCore is distinct — that means the identity
// itself no longer exists and propagates as a 404.
type GetOrHydrateUserUseCase struct {
	userRepo repository.UserRepository
	authcore authcore.Client
	ttl      time.Duration
	log      *logger.Logger
	now      func() time.Time
}

// NewGetOrHydrateUserUseCase constructs the use case. ttl==0 means the
// default TTL is used. log may be nil in tests.
func NewGetOrHydrateUserUseCase(userRepo repository.UserRepository, client authcore.Client, ttl time.Duration, log *logger.Logger) *GetOrHydrateUserUseCase {
	if ttl <= 0 {
		ttl = DefaultProfileTTL
	}
	return &GetOrHydrateUserUseCase{
		userRepo: userRepo,
		authcore: client,
		ttl:      ttl,
		log:      log,
		now:      time.Now,
	}
}

// Execute returns the user row for sub, creating or refreshing it when
// needed. The caller's AuthCore access token (if present on the context) is
// used to fetch the canonical profile; without a token (e.g. the caller is
// viewing someone else's profile) hydrate is fail-open and returns whatever
// is in the mirror.
func (uc *GetOrHydrateUserUseCase) Execute(ctx context.Context, sub string) (*domain.User, error) {
	if sub == "" {
		return nil, errors.InvalidRequest("sub is required", nil)
	}

	existing, err := uc.userRepo.GetBySub(ctx, sub)
	if err != nil {
		return nil, errors.DatabaseError("failed to get user", err)
	}

	accessToken, _ := auth.GetAccessTokenFromContext(ctx)
	now := uc.now()

	if existing == nil {
		return uc.lazyCreate(ctx, sub, accessToken, now)
	}

	if now.Sub(existing.ProfileRefreshedAt) < uc.ttl {
		return existing, nil
	}

	return uc.refresh(ctx, existing, accessToken, now)
}

// lazyCreate inserts a new mirror row for an authenticated sub the backend
// has not seen before.
func (uc *GetOrHydrateUserUseCase) lazyCreate(ctx context.Context, sub, accessToken string, now time.Time) (*domain.User, error) {
	user := &domain.User{Sub: sub, CreatedAt: now, UpdatedAt: now}

	if accessToken != "" {
		profile, perr := uc.authcore.GetProfile(ctx, accessToken)
		switch {
		case perr == nil:
			applyProfile(user, profile)
			user.ProfileRefreshedAt = now
		case stderrors.Is(perr, authcore.ErrNotFound):
			return nil, errors.NotFound("user not found in authcore")
		default:
			uc.logProfileFailure(ctx, sub, perr)
		}
	}

	saved, err := uc.userRepo.Upsert(ctx, user)
	if err != nil {
		return nil, errors.DatabaseError("failed to create user", err)
	}
	return saved, nil
}

// refresh updates cached profile fields when they are older than ttl. Fails
// open on AuthCore outages so the stale mirror is still returned.
func (uc *GetOrHydrateUserUseCase) refresh(ctx context.Context, existing *domain.User, accessToken string, now time.Time) (*domain.User, error) {
	if accessToken == "" {
		return existing, nil
	}

	profile, perr := uc.authcore.GetProfile(ctx, accessToken)
	if perr != nil {
		if stderrors.Is(perr, authcore.ErrNotFound) {
			return nil, errors.NotFound("user not found in authcore")
		}
		uc.logProfileFailure(ctx, existing.Sub, perr)
		return existing, nil
	}

	applyProfile(existing, profile)
	existing.ProfileRefreshedAt = now

	saved, err := uc.userRepo.Upsert(ctx, existing)
	if err != nil {
		return nil, errors.DatabaseError("failed to refresh user", err)
	}
	return saved, nil
}

// applyProfile copies AuthCore profile fields onto the mirror user. AuthCore
// has no display_name concept, so PublicID is used as a stand-in.
func applyProfile(user *domain.User, profile *authcore.Profile) {
	user.DisplayIDCached = profile.PublicID
	user.IconURLCached = profile.IconURL
	if user.DisplayNameCached == "" {
		user.DisplayNameCached = profile.PublicID
	}
}

func (uc *GetOrHydrateUserUseCase) logProfileFailure(ctx context.Context, sub string, err error) {
	if uc.log == nil {
		return
	}
	uc.log.Warn(ctx, "authcore profile fetch failed; returning stale mirror",
		"sub", sub,
		"error", err.Error(),
	)
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
// rejected.
func (uc *UpdateUserProfileUseCase) Execute(ctx context.Context, targetSub, currentSub string, req *domain.UpdateUserProfileRequest) (*domain.User, error) {
	if targetSub != currentSub {
		return nil, errors.Forbidden("you can only update your own profile")
	}
	if req == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}

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
