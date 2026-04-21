package follow

import (
	"context"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	apperrors "github.com/fuju/backend/pkg/errors"
)

type seedFn func(subs ...string)

func newFixture(t *testing.T) (*FollowUseCase, *UnfollowUseCase, *ListFollowersUseCase, *ListFollowingUseCase, seedFn) {
	t.Helper()
	userRepo := inmemory.NewUserRepository()
	followRepo := inmemory.NewFollowRepository()
	ctx := context.Background()
	seed := func(subs ...string) {
		for _, s := range subs {
			if _, err := userRepo.Upsert(ctx, &domain.User{Sub: s}); err != nil {
				t.Fatalf("seed %q: %v", s, err)
			}
		}
	}
	return NewFollowUseCase(userRepo, followRepo),
		NewUnfollowUseCase(userRepo, followRepo),
		NewListFollowersUseCase(userRepo, followRepo),
		NewListFollowingUseCase(userRepo, followRepo),
		seed
}

func TestFollow_Self_Rejected(t *testing.T) {
	follow, _, _, _, seed := newFixture(t)
	seed("u-a")
	_, err := follow.Execute(context.Background(), "u-a", "u-a")
	appErr, ok := apperrors.IsAppError(err)
	if !ok || appErr.Code != apperrors.ErrValidationFailed {
		t.Errorf("expected ValidationFailed, got %v", err)
	}
}

func TestFollow_MissingTarget_NotFound(t *testing.T) {
	follow, _, _, _, seed := newFixture(t)
	seed("u-a")
	_, err := follow.Execute(context.Background(), "u-a", "u-b")
	appErr, ok := apperrors.IsAppError(err)
	if !ok || appErr.Code != apperrors.ErrNotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestFollow_Idempotent(t *testing.T) {
	follow, _, _, _, seed := newFixture(t)
	seed("u-a", "u-b")
	ctx := context.Background()

	first, err := follow.Execute(ctx, "u-a", "u-b")
	if err != nil {
		t.Fatalf("first follow: %v", err)
	}
	if !first.Following || first.FollowersCount != 1 {
		t.Fatalf("first: %+v", first)
	}
	second, err := follow.Execute(ctx, "u-a", "u-b")
	if err != nil {
		t.Fatalf("second follow: %v", err)
	}
	if !second.Following || second.FollowersCount != 1 {
		t.Errorf("second follow should leave count at 1, got %+v", second)
	}
}

func TestUnfollow_Idempotent(t *testing.T) {
	follow, unfollow, _, _, seed := newFixture(t)
	seed("u-a", "u-b")
	ctx := context.Background()
	if _, err := follow.Execute(ctx, "u-a", "u-b"); err != nil {
		t.Fatalf("seed follow: %v", err)
	}
	res, err := unfollow.Execute(ctx, "u-a", "u-b")
	if err != nil || res.Following || res.FollowersCount != 0 {
		t.Fatalf("first unfollow: res=%+v err=%v", res, err)
	}
	// Second unfollow: no-op, counter stays at 0.
	res2, err := unfollow.Execute(ctx, "u-a", "u-b")
	if err != nil || res2.Following || res2.FollowersCount != 0 {
		t.Errorf("second unfollow: res=%+v err=%v", res2, err)
	}
}

func TestListFollowers_BasicOrder(t *testing.T) {
	follow, _, followers, _, seed := newFixture(t)
	seed("u-target", "u-a", "u-b", "u-c")
	ctx := context.Background()
	for _, s := range []string{"u-a", "u-b", "u-c"} {
		if _, err := follow.Execute(ctx, s, "u-target"); err != nil {
			t.Fatalf("seed follow %s: %v", s, err)
		}
	}

	users, next, err := followers.Execute(ctx, "u-target", nil, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 3 || next != "" {
		t.Fatalf("expected 3 users no next, got %d next=%q", len(users), next)
	}
}

func TestListFollowing_CursorPagination(t *testing.T) {
	follow, _, _, following, seed := newFixture(t)
	seed("u-me", "u-1", "u-2", "u-3")
	ctx := context.Background()
	for _, s := range []string{"u-1", "u-2", "u-3"} {
		if _, err := follow.Execute(ctx, "u-me", s); err != nil {
			t.Fatalf("follow %s: %v", s, err)
		}
	}

	page1, next1, err := following.Execute(ctx, "u-me", nil, 2)
	if err != nil || len(page1) != 2 || next1 == "" {
		t.Fatalf("page1: len=%d next=%q err=%v", len(page1), next1, err)
	}
	page2, next2, err := following.Execute(ctx, "u-me", &next1, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || next2 != "" {
		t.Errorf("page2: len=%d next=%q", len(page2), next2)
	}
}
