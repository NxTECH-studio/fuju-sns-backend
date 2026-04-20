package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/pkg/authcore"
)

type fakeAuthCore struct {
	profile *authcore.Profile
	err     error
	calls   int
}

func (f *fakeAuthCore) Introspect(_ context.Context, _ string) (*authcore.Session, error) {
	return nil, errors.New("not used")
}

func (f *fakeAuthCore) GetProfile(_ context.Context, sub string) (*authcore.Profile, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p := *f.profile
	p.Sub = sub
	return &p, nil
}

func TestGetOrHydrateUser_LazyCreate(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{profile: &authcore.Profile{DisplayName: "Alice", DisplayID: "alice", IconURL: "https://ex/a.png"}}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour)

	user, err := uc.Execute(context.Background(), "01HX")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if user.Sub != "01HX" || user.DisplayNameCached != "Alice" {
		t.Errorf("unexpected hydrated user: %+v", user)
	}
	if fake.calls != 1 {
		t.Errorf("expected 1 profile call, got %d", fake.calls)
	}

	// Second call within TTL should not re-hit authcore.
	if _, err := uc.Execute(context.Background(), "01HX"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("expected still 1 profile call, got %d", fake.calls)
	}
}

func TestGetOrHydrateUser_RefreshAfterTTL(t *testing.T) {
	repo := inmemory.NewUserRepository()
	fake := &fakeAuthCore{profile: &authcore.Profile{DisplayName: "Alice", DisplayID: "alice"}}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour)

	clock := time.Now()
	uc.now = func() time.Time { return clock }

	if _, err := uc.Execute(context.Background(), "01HX"); err != nil {
		t.Fatalf("first: %v", err)
	}

	// Simulate 2h passing and change the upstream profile.
	clock = clock.Add(2 * time.Hour)
	fake.profile = &authcore.Profile{DisplayName: "Alice B", DisplayID: "alice"}

	u, err := uc.Execute(context.Background(), "01HX")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if u.DisplayNameCached != "Alice B" {
		t.Errorf("expected refreshed DisplayName, got %q", u.DisplayNameCached)
	}
	if fake.calls != 2 {
		t.Errorf("expected 2 profile calls, got %d", fake.calls)
	}
}

func TestGetOrHydrateUser_FailOpenOnUpstream(t *testing.T) {
	repo := inmemory.NewUserRepository()
	// Pre-populate the mirror so we have something to return.
	if _, err := repo.Upsert(context.Background(), &domain.User{
		Sub:                "01HX",
		DisplayNameCached:  "Stale",
		ProfileRefreshedAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := &fakeAuthCore{err: authcore.ErrUpstream}
	uc := NewGetOrHydrateUserUseCase(repo, fake, time.Hour)

	u, err := uc.Execute(context.Background(), "01HX")
	if err != nil {
		t.Fatalf("expected fail-open, got err: %v", err)
	}
	if u.DisplayNameCached != "Stale" {
		t.Errorf("expected stale cache to be returned, got %q", u.DisplayNameCached)
	}
}

func TestUpsert_PreservesIsAdmin(t *testing.T) {
	repo := inmemory.NewUserRepository()
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, &domain.User{Sub: "01HX", IsAdmin: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate hydrate writing a refreshed row without explicitly setting IsAdmin.
	if _, err := repo.Upsert(ctx, &domain.User{Sub: "01HX", DisplayNameCached: "Alice"}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	got, err := repo.GetBySub(ctx, "01HX")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.IsAdmin {
		t.Errorf("expected IsAdmin to be preserved across Upsert")
	}
	if got.DisplayNameCached != "Alice" {
		t.Errorf("expected DisplayNameCached to be updated")
	}
}
