// Package post contains post business logic use cases.
package post

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
	"github.com/oklog/ulid/v2"
)

// DefaultPageLimit is the fallback list size when the caller does not
// specify one (or specifies something out of range).
const DefaultPageLimit = 20

// MaxPageLimit caps the per-request list size.
const MaxPageLimit = 50

// CreatePostUseCase creates a post with optional images and a parent
// reply reference. Tag extraction is delegated to a TagExtractor.
type CreatePostUseCase struct {
	postRepo     repository.PostRepository
	imageRepo    repository.ImageRepository
	tagRepo      repository.TagRepository
	tagExtractor domain.TagExtractor
}

// NewCreatePostUseCase constructs a CreatePostUseCase.
func NewCreatePostUseCase(
	postRepo repository.PostRepository,
	imageRepo repository.ImageRepository,
	tagRepo repository.TagRepository,
	tagExtractor domain.TagExtractor,
) *CreatePostUseCase {
	return &CreatePostUseCase{
		postRepo:     postRepo,
		imageRepo:    imageRepo,
		tagRepo:      tagRepo,
		tagExtractor: tagExtractor,
	}
}

// Execute validates, resolves the thread root, verifies image ownership,
// extracts tags, and persists the post.
func (uc *CreatePostUseCase) Execute(ctx context.Context, userSub string, req *domain.CreatePostRequest) (*PostDetail, error) {
	if userSub == "" {
		return nil, errors.Unauthorized("authentication required")
	}
	if req == nil {
		return nil, errors.InvalidRequest("request body is required", nil)
	}
	if err := req.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	var rootID *string
	if req.ParentPostID != nil {
		parent, err := uc.postRepo.GetByID(ctx, *req.ParentPostID)
		if err != nil {
			return nil, errors.DatabaseError("failed to load parent post", err)
		}
		if parent == nil {
			return nil, errors.NotFound("parent post not found")
		}
		// Inherit the thread root so ListReplies can find replies-of-replies
		// by the root id without walking the chain.
		if parent.RootPostID != nil {
			rootID = parent.RootPostID
		} else {
			id := parent.ID
			rootID = &id
		}
	}

	for _, imgID := range req.ImageIDs {
		img, err := uc.imageRepo.GetByID(ctx, imgID)
		if err != nil {
			return nil, errors.DatabaseError("failed to load image", err)
		}
		if img == nil {
			return nil, errors.NotFound("image not found")
		}
		if img.UserID != userSub {
			return nil, errors.Forbidden("cannot attach another user's image")
		}
	}

	tagNames, err := uc.tagExtractor.Extract(ctx, req.Content)
	if err != nil {
		return nil, errors.InternalServer("failed to extract tags", err)
	}
	tags, err := uc.tagRepo.UpsertByNames(ctx, tagNames)
	if err != nil {
		return nil, errors.DatabaseError("failed to upsert tags", err)
	}
	tagIDs := make([]string, len(tags))
	for i, t := range tags {
		tagIDs[i] = t.ID
	}

	post := &domain.Post{
		ID:           ulid.Make().String(),
		UserID:       userSub,
		Content:      req.Content,
		ParentPostID: req.ParentPostID,
		RootPostID:   rootID,
		Visibility:   "public",
	}
	if err := post.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	created, err := uc.postRepo.Create(ctx, post, req.ImageIDs, tagIDs)
	if err != nil {
		return nil, errors.DatabaseError("failed to create post", err)
	}

	if req.ParentPostID != nil {
		if err := uc.postRepo.IncrementRepliesCount(ctx, *req.ParentPostID); err != nil {
			return nil, errors.DatabaseError("failed to increment replies_count", err)
		}
	}

	images, err := uc.imageRepo.ListByPostID(ctx, created.ID)
	if err != nil {
		return nil, errors.DatabaseError("failed to load post images", err)
	}

	return &PostDetail{Post: created, Images: images, Tags: tags}, nil
}

// GetPostUseCase retrieves a post hydrated with its images, tags,
// author, and per-viewer liked / following flags.
type GetPostUseCase struct {
	postRepo repository.PostRepository
	hydrator *Hydrator
}

// NewGetPostUseCase constructs a GetPostUseCase.
func NewGetPostUseCase(postRepo repository.PostRepository, hydrator *Hydrator) *GetPostUseCase {
	return &GetPostUseCase{postRepo: postRepo, hydrator: hydrator}
}

// Execute returns a single PostDetail. viewerSub may be nil (anonymous).
func (uc *GetPostUseCase) Execute(ctx context.Context, postID string, viewerSub *string) (*PostDetail, error) {
	if postID == "" {
		return nil, errors.InvalidRequest("post id is required", nil)
	}
	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return nil, errors.DatabaseError("failed to load post", err)
	}
	if post == nil {
		return nil, errors.NotFound("post not found")
	}
	return uc.hydrator.HydrateOne(ctx, post, viewerSub)
}

// DeletePostUseCase soft-deletes a post. Only the author may delete.
type DeletePostUseCase struct {
	postRepo repository.PostRepository
}

// NewDeletePostUseCase constructs a DeletePostUseCase.
func NewDeletePostUseCase(postRepo repository.PostRepository) *DeletePostUseCase {
	return &DeletePostUseCase{postRepo: postRepo}
}

// Execute deletes the post and decrements the parent's replies_count if
// this was a reply.
func (uc *DeletePostUseCase) Execute(ctx context.Context, postID, userSub string) error {
	if userSub == "" {
		return errors.Unauthorized("authentication required")
	}
	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return errors.DatabaseError("failed to load post", err)
	}
	if post == nil {
		return errors.NotFound("post not found")
	}
	if post.UserID != userSub {
		return errors.Forbidden("you can only delete your own posts")
	}
	if err := uc.postRepo.Delete(ctx, postID); err != nil {
		return errors.DatabaseError("failed to delete post", err)
	}
	if post.ParentPostID != nil {
		if err := uc.postRepo.DecrementRepliesCount(ctx, *post.ParentPostID); err != nil {
			return errors.DatabaseError("failed to decrement replies_count", err)
		}
	}
	return nil
}

// ListPostsUseCase returns a cursor-paginated feed of posts. userID filters
// to a single author; viewerSub enables per-post likedByViewer.
type ListPostsUseCase struct {
	postRepo repository.PostRepository
	hydrator *Hydrator
}

// NewListPostsUseCase constructs a ListPostsUseCase.
func NewListPostsUseCase(postRepo repository.PostRepository, hydrator *Hydrator) *ListPostsUseCase {
	return &ListPostsUseCase{postRepo: postRepo, hydrator: hydrator}
}

// Execute returns a page of PostDetail and the next cursor (empty string
// when there are no more rows).
func (uc *ListPostsUseCase) Execute(ctx context.Context, userID *string, cursor *string, limit int, viewerSub *string) ([]*PostDetail, string, error) {
	limit = NormalizeLimit(limit)
	posts, nextCursor, err := uc.postRepo.List(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list posts", err)
	}
	details, err := uc.hydrator.Hydrate(ctx, posts, viewerSub)
	if err != nil {
		return nil, "", err
	}
	return details, nextCursor, nil
}

// ListRepliesUseCase returns a cursor-paginated list of a post's replies.
type ListRepliesUseCase struct {
	postRepo repository.PostRepository
	hydrator *Hydrator
}

// NewListRepliesUseCase constructs a ListRepliesUseCase.
func NewListRepliesUseCase(postRepo repository.PostRepository, hydrator *Hydrator) *ListRepliesUseCase {
	return &ListRepliesUseCase{postRepo: postRepo, hydrator: hydrator}
}

// Execute returns direct replies of postID.
func (uc *ListRepliesUseCase) Execute(ctx context.Context, postID string, cursor *string, limit int, viewerSub *string) ([]*PostDetail, string, error) {
	if postID == "" {
		return nil, "", errors.InvalidRequest("post id is required", nil)
	}
	limit = NormalizeLimit(limit)
	posts, nextCursor, err := uc.postRepo.ListReplies(ctx, postID, cursor, limit)
	if err != nil {
		return nil, "", errors.DatabaseError("failed to list replies", err)
	}
	details, err := uc.hydrator.Hydrate(ctx, posts, viewerSub)
	if err != nil {
		return nil, "", err
	}
	return details, nextCursor, nil
}

// LikePostUseCase marks a post as liked by the caller (idempotent).
type LikePostUseCase struct {
	postRepo repository.PostRepository
	likeRepo repository.LikeRepository
}

// NewLikePostUseCase constructs a LikePostUseCase.
func NewLikePostUseCase(postRepo repository.PostRepository, likeRepo repository.LikeRepository) *LikePostUseCase {
	return &LikePostUseCase{postRepo: postRepo, likeRepo: likeRepo}
}

// Execute likes the post. Repeated calls are no-ops (the denormalized
// likes_count is only incremented on the state transition 0→1).
func (uc *LikePostUseCase) Execute(ctx context.Context, userSub, postID string) error {
	if userSub == "" {
		return errors.Unauthorized("authentication required")
	}
	if postID == "" {
		return errors.InvalidRequest("post id is required", nil)
	}
	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return errors.DatabaseError("failed to load post", err)
	}
	if post == nil {
		return errors.NotFound("post not found")
	}
	inserted, err := uc.likeRepo.Create(ctx, userSub, postID)
	if err != nil {
		return errors.DatabaseError("failed to insert like", err)
	}
	if inserted {
		if err := uc.postRepo.IncrementLikesCount(ctx, postID); err != nil {
			return errors.DatabaseError("failed to increment likes_count", err)
		}
	}
	return nil
}

// UnlikePostUseCase removes the caller's like on a post (idempotent).
type UnlikePostUseCase struct {
	postRepo repository.PostRepository
	likeRepo repository.LikeRepository
}

// NewUnlikePostUseCase constructs an UnlikePostUseCase.
func NewUnlikePostUseCase(postRepo repository.PostRepository, likeRepo repository.LikeRepository) *UnlikePostUseCase {
	return &UnlikePostUseCase{postRepo: postRepo, likeRepo: likeRepo}
}

// Execute unlikes the post. Repeated calls are no-ops.
func (uc *UnlikePostUseCase) Execute(ctx context.Context, userSub, postID string) error {
	if userSub == "" {
		return errors.Unauthorized("authentication required")
	}
	if postID == "" {
		return errors.InvalidRequest("post id is required", nil)
	}
	deleted, err := uc.likeRepo.Delete(ctx, userSub, postID)
	if err != nil {
		return errors.DatabaseError("failed to delete like", err)
	}
	if deleted {
		if err := uc.postRepo.DecrementLikesCount(ctx, postID); err != nil {
			return errors.DatabaseError("failed to decrement likes_count", err)
		}
	}
	return nil
}

// NormalizeLimit clamps a user-supplied limit to the package's
// [1, MaxPageLimit] range, falling back to DefaultPageLimit on out-of-range
// values. Exported so the timeline usecases can reuse the same policy.
func NormalizeLimit(limit int) int {
	if limit <= 0 || limit > MaxPageLimit {
		return DefaultPageLimit
	}
	return limit
}
