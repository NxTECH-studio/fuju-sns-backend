package timeline

import (
	"context"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	postusecase "github.com/fuju/backend/internal/usecase/post"
)

type noopExtractor struct{}

func (noopExtractor) Extract(_ context.Context, _ string) ([]string, error) { return nil, nil }

type tlFixture struct {
	userRepo interface {
		Upsert(context.Context, *domain.User) (*domain.User, error)
	}
	createPost *postusecase.CreatePostUseCase
	follow     func(follower, followee string)
	home       *HomeTimelineUseCase
	userTL     *UserTimelineUseCase
	global     *GlobalTimelineUseCase
}

func newFixture(t *testing.T) *tlFixture {
	t.Helper()
	links := inmemory.NewLinkStore()
	postRepo := inmemory.NewPostRepository(links)
	imageRepo := inmemory.NewImageRepository(links)
	tagRepo := inmemory.NewTagRepository(links)
	likeRepo := inmemory.NewLikeRepository()
	userRepo := inmemory.NewUserRepository()
	followRepo := inmemory.NewFollowRepository()
	hyd := postusecase.NewHydrator(imageRepo, tagRepo, likeRepo, userRepo, followRepo)

	createPost := postusecase.NewCreatePostUseCase(postRepo, imageRepo, tagRepo, noopExtractor{})
	follow := func(follower, followee string) {
		if _, err := followRepo.Create(context.Background(), follower, followee); err != nil {
			t.Fatalf("follow %s->%s: %v", follower, followee, err)
		}
	}

	return &tlFixture{
		userRepo:   userRepo,
		createPost: createPost,
		follow:     follow,
		home:       NewHomeTimelineUseCase(postRepo, followRepo, hyd),
		userTL:     NewUserTimelineUseCase(postRepo, hyd),
		global:     NewGlobalTimelineUseCase(postRepo, hyd),
	}
}

func (f *tlFixture) seedUser(t *testing.T, sub string) {
	t.Helper()
	if _, err := f.userRepo.Upsert(context.Background(), &domain.User{Sub: sub}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func (f *tlFixture) seedPost(t *testing.T, author string) string {
	t.Helper()
	d, err := f.createPost.Execute(context.Background(), author, &domain.CreatePostRequest{Content: "p"})
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	return d.Post.ID
}

func TestHomeTimeline_IncludesSelfOnly_WhenNoFollows(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "me")
	f.seedUser(t, "other")
	mine := f.seedPost(t, "me")
	_ = f.seedPost(t, "other")

	details, next, err := f.home.Execute(context.Background(), "me", nil, 20)
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if len(details) != 1 || details[0].Post.ID != mine {
		t.Errorf("expected only self post %q, got %+v", mine, details)
	}
	if next != "" {
		t.Errorf("expected empty next cursor, got %q", next)
	}
}

func TestHomeTimeline_IncludesFollowedAuthors(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "me")
	f.seedUser(t, "friend")
	f.seedUser(t, "stranger")
	f.follow("me", "friend")

	mine := f.seedPost(t, "me")
	theirs := f.seedPost(t, "friend")
	_ = f.seedPost(t, "stranger")

	details, _, err := f.home.Execute(context.Background(), "me", nil, 20)
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	ids := make(map[string]bool, len(details))
	for _, d := range details {
		ids[d.Post.ID] = true
	}
	if !ids[mine] || !ids[theirs] || len(details) != 2 {
		t.Errorf("home should contain {%q, %q} only, got %v", mine, theirs, ids)
	}
}

func TestGlobalTimeline_IncludesEveryone(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "a")
	f.seedUser(t, "b")
	f.seedPost(t, "a")
	f.seedPost(t, "b")

	details, _, err := f.global.Execute(context.Background(), nil, 20, nil)
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	if len(details) != 2 {
		t.Errorf("global expected 2 posts, got %d", len(details))
	}
}

func TestUserTimeline_OnlyTargetAuthor(t *testing.T) {
	f := newFixture(t)
	f.seedUser(t, "a")
	f.seedUser(t, "b")
	mine := f.seedPost(t, "a")
	_ = f.seedPost(t, "b")

	details, _, err := f.userTL.Execute(context.Background(), "a", nil, 20, nil)
	if err != nil {
		t.Fatalf("user tl: %v", err)
	}
	if len(details) != 1 || details[0].Post.ID != mine {
		t.Errorf("user TL expected %q only, got %+v", mine, details)
	}
}
