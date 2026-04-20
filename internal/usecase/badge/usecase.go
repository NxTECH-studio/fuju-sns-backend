// Package badge contains badge business logic use cases: reading a user's
// active badges, admin-only grant/revoke, and master CRUD.
package badge

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/internal/usecase/admin"
	"github.com/fuju/backend/pkg/errors"
	"github.com/oklog/ulid/v2"
)

// ensureAdmin is the shared "require admin privilege" guard used by every
// state-mutating badge usecase. It is defense-in-depth over AdminMiddleware —
// usecases may be reached from non-HTTP surfaces in the future, so the check
// must not rely solely on the middleware layer.
func ensureAdmin(ctx context.Context, checker *admin.Checker, sub string) error {
	if sub == "" {
		return errors.Unauthorized("authentication required")
	}
	ok, err := checker.IsAdmin(ctx, sub)
	if err != nil {
		return err
	}
	if !ok {
		return errors.Forbidden("admin privilege required")
	}
	return nil
}

// GetUserBadgesUseCase returns the badges currently granted to a user, in
// priority order. Expired grants are filtered by the repository.
type GetUserBadgesUseCase struct {
	badgeRepo repository.BadgeRepository
}

// NewGetUserBadgesUseCase constructs a GetUserBadgesUseCase.
func NewGetUserBadgesUseCase(badgeRepo repository.BadgeRepository) *GetUserBadgesUseCase {
	return &GetUserBadgesUseCase{badgeRepo: badgeRepo}
}

// Execute returns the badges for sub. Callers that receive an empty slice
// should still surface an empty `badges: []` field in responses.
func (uc *GetUserBadgesUseCase) Execute(ctx context.Context, sub string) ([]*domain.Badge, error) {
	if sub == "" {
		return nil, errors.InvalidRequest("sub is required", nil)
	}
	badges, err := uc.badgeRepo.ListByUserID(ctx, sub)
	if err != nil {
		return nil, errors.DatabaseError("failed to list user badges", err)
	}
	return badges, nil
}

// ListUserBadgesBatchUseCase returns active badges for a batch of users,
// keyed by sub. Missing entries in the returned map mean "no active badges".
// Enables list endpoints to embed badges without N+1.
type ListUserBadgesBatchUseCase struct {
	badgeRepo repository.BadgeRepository
}

// NewListUserBadgesBatchUseCase constructs a ListUserBadgesBatchUseCase.
func NewListUserBadgesBatchUseCase(badgeRepo repository.BadgeRepository) *ListUserBadgesBatchUseCase {
	return &ListUserBadgesBatchUseCase{badgeRepo: badgeRepo}
}

// Execute returns a map sub -> badges for every sub in subs. Empty input
// returns an empty map (not an error).
func (uc *ListUserBadgesBatchUseCase) Execute(ctx context.Context, subs []string) (map[string][]*domain.Badge, error) {
	if len(subs) == 0 {
		return map[string][]*domain.Badge{}, nil
	}
	out, err := uc.badgeRepo.ListByUserIDs(ctx, subs)
	if err != nil {
		return nil, errors.DatabaseError("failed to list user badges", err)
	}
	return out, nil
}

// GrantBadgeUseCase is the admin-only "attach a badge to a user" flow.
type GrantBadgeUseCase struct {
	badgeRepo repository.BadgeRepository
	userRepo  repository.UserRepository
	checker   *admin.Checker
}

// NewGrantBadgeUseCase constructs a GrantBadgeUseCase.
func NewGrantBadgeUseCase(badgeRepo repository.BadgeRepository, userRepo repository.UserRepository, checker *admin.Checker) *GrantBadgeUseCase {
	return &GrantBadgeUseCase{badgeRepo: badgeRepo, userRepo: userRepo, checker: checker}
}

// Execute grants req.BadgeKey to targetSub. Returns the resolved badge so the
// caller can echo it back.
func (uc *GrantBadgeUseCase) Execute(ctx context.Context, adminSub, targetSub string, req *domain.GrantBadgeRequest) (*domain.Badge, error) {
	if err := ensureAdmin(ctx, uc.checker, adminSub); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}
	if targetSub == "" {
		return nil, errors.InvalidRequest("target sub is required", nil)
	}
	if err := req.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	badge, err := uc.badgeRepo.GetByKey(ctx, req.BadgeKey)
	if err != nil {
		return nil, errors.DatabaseError("failed to load badge", err)
	}
	if badge == nil {
		return nil, errors.NotFound("badge not found")
	}

	user, err := uc.userRepo.GetBySub(ctx, targetSub)
	if err != nil {
		return nil, errors.DatabaseError("failed to load user", err)
	}
	if user == nil {
		return nil, errors.NotFound("user not found")
	}

	if err := uc.badgeRepo.Grant(ctx, targetSub, badge.ID, adminSub, req.ExpiresAt, req.Reason); err != nil {
		return nil, errors.DatabaseError("failed to grant badge", err)
	}
	return badge, nil
}

// RevokeBadgeUseCase is the admin-only "detach a badge from a user" flow.
// Revoking a non-existent grant is a no-op (MVP idempotent semantics).
type RevokeBadgeUseCase struct {
	badgeRepo repository.BadgeRepository
	checker   *admin.Checker
}

// NewRevokeBadgeUseCase constructs a RevokeBadgeUseCase.
func NewRevokeBadgeUseCase(badgeRepo repository.BadgeRepository, checker *admin.Checker) *RevokeBadgeUseCase {
	return &RevokeBadgeUseCase{badgeRepo: badgeRepo, checker: checker}
}

// Execute revokes badgeID from targetSub.
func (uc *RevokeBadgeUseCase) Execute(ctx context.Context, adminSub, targetSub, badgeID string) error {
	if err := ensureAdmin(ctx, uc.checker, adminSub); err != nil {
		return err
	}
	if targetSub == "" || badgeID == "" {
		return errors.InvalidRequest("target sub and badge id are required", nil)
	}
	if err := uc.badgeRepo.Revoke(ctx, targetSub, badgeID); err != nil {
		return errors.DatabaseError("failed to revoke badge", err)
	}
	return nil
}

// ListBadgesUseCase returns all master badges (admin list endpoint).
type ListBadgesUseCase struct {
	badgeRepo repository.BadgeRepository
}

// NewListBadgesUseCase constructs a ListBadgesUseCase.
func NewListBadgesUseCase(badgeRepo repository.BadgeRepository) *ListBadgesUseCase {
	return &ListBadgesUseCase{badgeRepo: badgeRepo}
}

// Execute returns the full master list.
func (uc *ListBadgesUseCase) Execute(ctx context.Context) ([]*domain.Badge, error) {
	badges, err := uc.badgeRepo.ListAll(ctx)
	if err != nil {
		return nil, errors.DatabaseError("failed to list badges", err)
	}
	return badges, nil
}

// CreateBadgeUseCase inserts a new master badge. Admin only.
type CreateBadgeUseCase struct {
	badgeRepo repository.BadgeRepository
	checker   *admin.Checker
}

// NewCreateBadgeUseCase constructs a CreateBadgeUseCase.
func NewCreateBadgeUseCase(badgeRepo repository.BadgeRepository, checker *admin.Checker) *CreateBadgeUseCase {
	return &CreateBadgeUseCase{badgeRepo: badgeRepo, checker: checker}
}

// Execute validates, assigns a ULID, and inserts a new master badge.
func (uc *CreateBadgeUseCase) Execute(ctx context.Context, adminSub string, badge *domain.Badge) (*domain.Badge, error) {
	if err := ensureAdmin(ctx, uc.checker, adminSub); err != nil {
		return nil, err
	}
	if badge == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}
	if err := badge.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	existing, err := uc.badgeRepo.GetByKey(ctx, badge.Key)
	if err != nil {
		return nil, errors.DatabaseError("failed to check badge uniqueness", err)
	}
	if existing != nil {
		return nil, errors.Conflict("badge key already exists")
	}

	badge.ID = ulid.Make().String()
	created, err := uc.badgeRepo.Create(ctx, badge)
	if err != nil {
		return nil, errors.DatabaseError("failed to create badge", err)
	}
	if created == nil {
		return nil, errors.Conflict("badge key already exists")
	}
	return created, nil
}

// UpdateBadgeUseCase updates the mutable fields of an existing master badge.
// Admin only.
type UpdateBadgeUseCase struct {
	badgeRepo repository.BadgeRepository
	checker   *admin.Checker
}

// NewUpdateBadgeUseCase constructs an UpdateBadgeUseCase.
func NewUpdateBadgeUseCase(badgeRepo repository.BadgeRepository, checker *admin.Checker) *UpdateBadgeUseCase {
	return &UpdateBadgeUseCase{badgeRepo: badgeRepo, checker: checker}
}

// Execute updates the addressed badge with fields from `patch`. Unchanged
// fields (Key) remain as stored.
func (uc *UpdateBadgeUseCase) Execute(ctx context.Context, adminSub, badgeID string, patch *domain.Badge) (*domain.Badge, error) {
	if err := ensureAdmin(ctx, uc.checker, adminSub); err != nil {
		return nil, err
	}
	if patch == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}

	existing, err := uc.badgeRepo.GetByID(ctx, badgeID)
	if err != nil {
		return nil, errors.DatabaseError("failed to load badge", err)
	}
	if existing == nil {
		return nil, errors.NotFound("badge not found")
	}

	existing.Label = patch.Label
	existing.Description = patch.Description
	existing.IconURL = patch.IconURL
	existing.Color = patch.Color
	existing.Priority = patch.Priority
	if err := existing.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	updated, err := uc.badgeRepo.Update(ctx, existing)
	if err != nil {
		return nil, errors.DatabaseError("failed to update badge", err)
	}
	if updated == nil {
		return nil, errors.NotFound("badge not found")
	}
	return updated, nil
}
