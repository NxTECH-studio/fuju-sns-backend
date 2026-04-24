package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/authcore"
	apperrors "github.com/fuju/backend/pkg/errors"
)

type fakeAuthCore struct {
	profile *authcore.Profile
	err     error
	calls   int
}

func (f *fakeAuthCore) Introspect(_ context.Context, _ string) (*authcore.Session, error) {
	return nil, errors.New("not used")
}

func (f *fakeAuthCore) GetProfile(_ context.Context, _ string) (*authcore.Profile, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p := *f.profile
	return &p, nil
}

func authedCtx(token string) context.Context {
	return auth.SetAccessTokenInContext(context.Background(), token)
}

func TestGetOrHydrateUser_LazyCreate(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{profile: &authcore.Profile{Sub: "01HX", PublicID: "alice", IconURL: "https://ex/a.png"}}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour, nil)

	ctx := authedCtx("access-tok")
	user, err := uc.Execute(ctx, "01HX")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if user.Sub != "01HX" || user.DisplayIDCached != "alice" || user.IconURLCached != "https://ex/a.png" {
		t.Errorf("unexpected hydrated user: %+v", user)
	}
	if fake.calls != 1 {
		t.Errorf("expected 1 profile call, got %d", fake.calls)
	}

	// Second call within TTL — no upstream hit.
	if _, err := uc.Execute(ctx, "01HX"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("expected still 1 profile call, got %d", fake.calls)
	}
}

func TestGetOrHydrateUser_LazyCreate_AuthCoreNotFound(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{err: authcore.ErrNotFound}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour, nil)

	_, err := uc.Execute(authedCtx("tok"), "01HX")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := apperrors.IsAppError(err)
	if !ok || appErr.Code != apperrors.ErrNotFound {
		t.Errorf("expected NotFound AppError, got %v", err)
	}
}

func TestGetOrHydrateUser_LazyCreate_UpstreamFailsOpen(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{err: authcore.ErrUpstream}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour, nil)

	user, err := uc.Execute(authedCtx("tok"), "01HX")
	if err != nil {
		t.Fatalf("expected fail-open insert, got err: %v", err)
	}
	if user.Sub != "01HX" {
		t.Errorf("expected minimal row with sub, got %+v", user)
	}
	if !user.ProfileRefreshedAt.IsZero() {
		t.Errorf("ProfileRefreshedAt should stay zero so next call retries, got %v", user.ProfileRefreshedAt)
	}
}

func TestGetOrHydrateUser_RefreshAfterTTL(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{profile: &authcore.Profile{Sub: "01HX", PublicID: "alice"}}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour, nil)

	clock := time.Now()
	uc.now = func() time.Time { return clock }
	ctx := authedCtx("tok")

	if _, err := uc.Execute(ctx, "01HX"); err != nil {
		t.Fatalf("first: %v", err)
	}

	clock = clock.Add(2 * time.Hour)
	fake.profile = &authcore.Profile{Sub: "01HX", PublicID: "alice-b"}

	u, err := uc.Execute(ctx, "01HX")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if u.DisplayIDCached != "alice-b" {
		t.Errorf("expected refreshed DisplayIDCached, got %q", u.DisplayIDCached)
	}
	if fake.calls != 2 {
		t.Errorf("expected 2 profile calls, got %d", fake.calls)
	}
}

func TestGetOrHydrateUser_RefreshFailOpen(t *testing.T) {
	repo := inmemory.NewUserRepository()
	if _, err := repo.Upsert(context.Background(), &domain.User{
		Sub:                "01HX",
		DisplayIDCached:    "stale",
		ProfileRefreshedAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := &fakeAuthCore{err: authcore.ErrUpstream}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour, nil)

	u, err := uc.Execute(authedCtx("tok"), "01HX")
	if err != nil {
		t.Fatalf("expected fail-open, got err: %v", err)
	}
	if u.DisplayIDCached != "stale" {
		t.Errorf("expected stale cache to be returned, got %q", u.DisplayIDCached)
	}
}

func TestUpsert_PreservesIsAdminAndSNSFields(t *testing.T) {
	repo := inmemory.NewUserRepository()
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, &domain.User{
		Sub:       "01HX",
		IsAdmin:   true,
		Bio:       "hello",
		BannerURL: "https://ex/b.png",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate hydrate writing a refreshed row with only cached fields set.
	if _, err := repo.Upsert(ctx, &domain.User{
		Sub:             "01HX",
		DisplayIDCached: "alice",
	}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	got, err := repo.GetBySub(ctx, "01HX")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.IsAdmin {
		t.Errorf("IsAdmin should be preserved")
	}
	if got.Bio != "hello" {
		t.Errorf("Bio should be preserved, got %q", got.Bio)
	}
	if got.BannerURL != "https://ex/b.png" {
		t.Errorf("BannerURL should be preserved, got %q", got.BannerURL)
	}
	if got.DisplayIDCached != "alice" {
		t.Errorf("DisplayIDCached should be updated")
	}
}
