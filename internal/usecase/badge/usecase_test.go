package badge

import (
	"context"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/internal/usecase/admin"
	apperrors "github.com/fuju/backend/pkg/errors"
)

// newFixture wires an in-memory stack with one admin sub and one normal sub,
// seeds both users, and seeds the `developer` master badge.
func newFixture(t *testing.T) (*testFixture, context.Context) {
	t.Helper()
	ctx := context.Background()

	const adminSub = "01ADMINADMINADMINADMINADMN"
	const userSub = "01USERUSERUSERUSERUSERUSR1"

	userRepo := inmemory.NewUserRepository()
	if _, err := userRepo.Upsert(ctx, &domain.User{Sub: adminSub, IsAdmin: true}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if _, err := userRepo.Upsert(ctx, &domain.User{Sub: userSub}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	badgeRepo := inmemory.NewBadgeRepository()
	devBadge, err := badgeRepo.Create(ctx, &domain.Badge{
		ID: "01BADGEDEV0000000000000000", Key: "developer", Label: "開発者", Color: "gold", Priority: 5,
	})
	if err != nil {
		t.Fatalf("seed badge: %v", err)
	}

	checker := admin.NewChecker(userRepo)
	return &testFixture{
		adminSub:  adminSub,
		userSub:   userSub,
		devBadge:  devBadge,
		userRepo:  userRepo,
		badgeRepo: badgeRepo,
		grantUC:   NewGrantBadgeUseCase(badgeRepo, userRepo, checker),
		revokeUC:  NewRevokeBadgeUseCase(badgeRepo, checker),
		getUC:     NewGetUserBadgesUseCase(badgeRepo),
	}, ctx
}

type testFixture struct {
	adminSub, userSub string
	devBadge          *domain.Badge
	userRepo          interface{}
	badgeRepo         interface{}
	grantUC           *GrantBadgeUseCase
	revokeUC          *RevokeBadgeUseCase
	getUC             *GetUserBadgesUseCase
}

func TestGrant_NonAdminIsForbidden(t *testing.T) {
	f, ctx := newFixture(t)

	_, err := f.grantUC.Execute(ctx, f.userSub, f.userSub, &domain.GrantBadgeRequest{BadgeKey: "developer"})
	assertAppErr(t, err, apperrors.ErrForbidden)
}

func TestGrant_UnknownBadgeKey(t *testing.T) {
	f, ctx := newFixture(t)

	_, err := f.grantUC.Execute(ctx, f.adminSub, f.userSub, &domain.GrantBadgeRequest{BadgeKey: "ghost"})
	assertAppErr(t, err, apperrors.ErrNotFound)
}

func TestGrant_UnknownUser(t *testing.T) {
	f, ctx := newFixture(t)

	_, err := f.grantUC.Execute(ctx, f.adminSub, "01MISSINGMISSINGMISSINGMSS", &domain.GrantBadgeRequest{BadgeKey: "developer"})
	assertAppErr(t, err, apperrors.ErrNotFound)
}

func TestGrant_Succeeds(t *testing.T) {
	f, ctx := newFixture(t)

	badge, err := f.grantUC.Execute(ctx, f.adminSub, f.userSub, &domain.GrantBadgeRequest{BadgeKey: "developer"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if badge.Key != "developer" {
		t.Errorf("expected developer badge back, got %q", badge.Key)
	}

	got, err := f.getUC.Execute(ctx, f.userSub)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Key != "developer" {
		t.Errorf("expected [developer], got %+v", got)
	}
}

func TestRevoke_NonAdminIsForbidden(t *testing.T) {
	f, ctx := newFixture(t)

	err := f.revokeUC.Execute(ctx, f.userSub, f.userSub, f.devBadge.ID)
	assertAppErr(t, err, apperrors.ErrForbidden)
}

func TestRevoke_Idempotent(t *testing.T) {
	f, ctx := newFixture(t)

	// Revoke without having granted first — MVP accepts this as a no-op.
	if err := f.revokeUC.Execute(ctx, f.adminSub, f.userSub, f.devBadge.ID); err != nil {
		t.Fatalf("revoke missing: %v", err)
	}
}

func TestGetUserBadges_FiltersExpired(t *testing.T) {
	f, ctx := newFixture(t)

	// Grant expired.
	past := time.Now().Add(-time.Hour)
	if _, err := f.grantUC.Execute(ctx, f.adminSub, f.userSub, &domain.GrantBadgeRequest{
		BadgeKey:  "developer",
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	got, err := f.getUC.Execute(ctx, f.userSub)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected expired badge to be filtered, got %+v", got)
	}
}

func assertAppErr(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected err %s, got nil", wantCode)
	}
	appErr, ok := apperrors.IsAppError(err)
	if !ok {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("expected code %s, got %s", wantCode, appErr.Code)
	}
}
