package post

import (
	"context"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	apperrors "github.com/fuju/backend/pkg/errors"
)

// fakeExtractor satisfies domain.TagExtractor without regex machinery so
// usecase tests can assert tag plumbing independent of extractor behavior.
type fakeExtractor struct {
	tags []string
	err  error
}

func (f *fakeExtractor) Extract(_ context.Context, _ string) ([]string, error) {
	return f.tags, f.err
}

type postFixture struct {
	links    *inmemory.LinkStore
	postRepo interface { /* filled below */
	}
	imageRepo interface{}
	tagRepo   interface{}
	likeRepo  interface{}
	create    *CreatePostUseCase
	del       *DeletePostUseCase
	list      *ListPostsUseCase
	like      *LikePostUseCase
	unlike    *UnlikePostUseCase
}

func newFixture(t *testing.T, tags []string) *postFixture {
	t.Helper()
	links := inmemory.NewLinkStore()
	postRepo := inmemory.NewPostRepository(links)
	imageRepo := inmemory.NewImageRepository(links)
	tagRepo := inmemory.NewTagRepository(links)
	likeRepo := inmemory.NewLikeRepository()
	extractor := &fakeExtractor{tags: tags}
	return &postFixture{
		links:     links,
		postRepo:  postRepo,
		imageRepo: imageRepo,
		tagRepo:   tagRepo,
		likeRepo:  likeRepo,
		create:    NewCreatePostUseCase(postRepo, imageRepo, tagRepo, extractor),
		del:       NewDeletePostUseCase(postRepo),
		list:      NewListPostsUseCase(postRepo, imageRepo, tagRepo, likeRepo),
		like:      NewLikePostUseCase(postRepo, likeRepo),
		unlike:    NewUnlikePostUseCase(postRepo, likeRepo),
	}
}

func TestCreatePost_ImageOwnerCheck(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	// Seed an image owned by someone else.
	imageRepo := f.imageRepo.(interface {
		Create(context.Context, *domain.Image) (*domain.Image, error)
	})
	if _, err := imageRepo.Create(ctx, &domain.Image{ID: "img-1", UserID: "other-user"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	_, err := f.create.Execute(ctx, "attacker-sub", &domain.CreatePostRequest{
		Content:  "hello",
		ImageIDs: []string{"img-1"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := apperrors.IsAppError(err)
	if !ok || appErr.Code != apperrors.ErrForbidden {
		t.Errorf("expected Forbidden, got %v", err)
	}
}

func TestCreatePost_RootInheritance(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	// Top-level post by user A.
	top, err := f.create.Execute(ctx, "user-a", &domain.CreatePostRequest{Content: "top"})
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if top.Post.RootPostID != nil {
		t.Errorf("top-level post should have RootPostID == nil, got %v", *top.Post.RootPostID)
	}

	// Reply by user B. RootPostID should point at `top`.
	topID := top.Post.ID
	reply, err := f.create.Execute(ctx, "user-b", &domain.CreatePostRequest{
		Content:      "reply",
		ParentPostID: &topID,
	})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.Post.RootPostID == nil || *reply.Post.RootPostID != topID {
		t.Errorf("reply RootPostID should be %q, got %v", topID, reply.Post.RootPostID)
	}

	// Reply to reply. RootPostID should still be `top`.
	replyID := reply.Post.ID
	r2, err := f.create.Execute(ctx, "user-c", &domain.CreatePostRequest{
		Content:      "reply to reply",
		ParentPostID: &replyID,
	})
	if err != nil {
		t.Fatalf("r2: %v", err)
	}
	if r2.Post.RootPostID == nil || *r2.Post.RootPostID != topID {
		t.Errorf("nested reply RootPostID should be %q, got %v", topID, r2.Post.RootPostID)
	}
}

func TestCreatePost_IncrementsParentRepliesCount(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	top, err := f.create.Execute(ctx, "user-a", &domain.CreatePostRequest{Content: "top"})
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	parentID := top.Post.ID
	if _, err := f.create.Execute(ctx, "user-b", &domain.CreatePostRequest{Content: "r1", ParentPostID: &parentID}); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, err := f.create.Execute(ctx, "user-c", &domain.CreatePostRequest{Content: "r2", ParentPostID: &parentID}); err != nil {
		t.Fatalf("r2: %v", err)
	}
	postRepo := f.postRepo.(interface {
		GetByID(context.Context, string) (*domain.Post, error)
	})
	refreshed, err := postRepo.GetByID(ctx, parentID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if refreshed.RepliesCount != 2 {
		t.Errorf("expected RepliesCount=2, got %d", refreshed.RepliesCount)
	}
}

func TestDeletePost_Forbidden(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	d, err := f.create.Execute(ctx, "owner", &domain.CreatePostRequest{Content: "mine"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	err = f.del.Execute(ctx, d.Post.ID, "someone-else")
	appErr, ok := apperrors.IsAppError(err)
	if !ok || appErr.Code != apperrors.ErrForbidden {
		t.Errorf("expected Forbidden, got %v", err)
	}
}

func TestListPosts_CursorPagination(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	// Insert 5 posts.
	var ids []string
	for i := 0; i < 5; i++ {
		d, err := f.create.Execute(ctx, "u", &domain.CreatePostRequest{Content: "p"})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, d.Post.ID)
	}

	// First page of 2. Because ULIDs are monotonic ascending, most-recent first
	// = ids[4], ids[3].
	page1, next1, err := f.list.Execute(ctx, nil, nil, 2, nil)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len: %d", len(page1))
	}
	if page1[0].Post.ID != ids[4] || page1[1].Post.ID != ids[3] {
		t.Errorf("page1 ids = %v %v, want %v %v", page1[0].Post.ID, page1[1].Post.ID, ids[4], ids[3])
	}
	if next1 == "" {
		t.Fatal("expected next cursor on middle page")
	}

	// Second page of 2 using next1 as cursor.
	page2, next2, err := f.list.Execute(ctx, nil, &next1, 2, nil)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len: %d", len(page2))
	}
	if page2[0].Post.ID != ids[2] || page2[1].Post.ID != ids[1] {
		t.Errorf("page2 ids = %v %v, want %v %v", page2[0].Post.ID, page2[1].Post.ID, ids[2], ids[1])
	}

	// Third page returns the tail element, next cursor empty.
	page3, next3, err := f.list.Execute(ctx, nil, &next2, 2, nil)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3) != 1 || page3[0].Post.ID != ids[0] {
		t.Errorf("page3 = %v, want single %v", page3, ids[0])
	}
	if next3 != "" {
		t.Errorf("expected empty next cursor on tail, got %q", next3)
	}
}

func TestLikeUnlike_Idempotent(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	d, err := f.create.Execute(ctx, "author", &domain.CreatePostRequest{Content: "p"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := d.Post.ID

	// Like twice: counter must only bump once.
	if err := f.like.Execute(ctx, "viewer", id); err != nil {
		t.Fatalf("like1: %v", err)
	}
	if err := f.like.Execute(ctx, "viewer", id); err != nil {
		t.Fatalf("like2: %v", err)
	}
	postRepo := f.postRepo.(interface {
		GetByID(context.Context, string) (*domain.Post, error)
	})
	p, _ := postRepo.GetByID(ctx, id)
	if p.LikesCount != 1 {
		t.Errorf("expected LikesCount=1 after double-like, got %d", p.LikesCount)
	}

	// Unlike twice: counter must only drop once.
	if err := f.unlike.Execute(ctx, "viewer", id); err != nil {
		t.Fatalf("unlike1: %v", err)
	}
	if err := f.unlike.Execute(ctx, "viewer", id); err != nil {
		t.Fatalf("unlike2: %v", err)
	}
	p, _ = postRepo.GetByID(ctx, id)
	if p.LikesCount != 0 {
		t.Errorf("expected LikesCount=0 after double-unlike, got %d", p.LikesCount)
	}
}

func TestCreatePost_UsesTagExtractor(t *testing.T) {
	f := newFixture(t, []string{"golang", "fuju"})
	ctx := context.Background()
	d, err := f.create.Execute(ctx, "u", &domain.CreatePostRequest{Content: "hi #golang #fuju"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(d.Tags), d.Tags)
	}
	seen := map[string]bool{}
	for _, tag := range d.Tags {
		seen[tag.Name] = true
	}
	if !seen["golang"] || !seen["fuju"] {
		t.Errorf("expected {golang, fuju}, got %v", seen)
	}
}
