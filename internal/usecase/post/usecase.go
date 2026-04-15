// Package post contains post business logic use cases.
package post

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
)

// GetPostUseCase represents the use case for getting a post
type GetPostUseCase struct {
	postRepo repository.PostRepository
}

// NewGetPostUseCase creates a new GetPostUseCase
func NewGetPostUseCase(postRepo repository.PostRepository) *GetPostUseCase {
	return &GetPostUseCase{postRepo: postRepo}
}

// Execute retrieves a post by ID
func (uc *GetPostUseCase) Execute(ctx context.Context, postID int64) (*domain.Post, error) {
	if postID <= 0 {
		return nil, errors.InvalidRequest("invalid post ID", nil)
	}

	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return nil, errors.DatabaseError("failed to get post", err)
	}

	if post == nil {
		return nil, errors.NotFound("post not found")
	}

	return post, nil
}

// CreatePostUseCase represents the use case for creating a post
type CreatePostUseCase struct {
	postRepo repository.PostRepository
}

// NewCreatePostUseCase creates a new CreatePostUseCase
func NewCreatePostUseCase(postRepo repository.PostRepository) *CreatePostUseCase {
	return &CreatePostUseCase{postRepo: postRepo}
}

// Execute creates a new post
func (uc *CreatePostUseCase) Execute(ctx context.Context, userID int64, req *domain.CreatePostRequest) (*domain.Post, error) {
	post := &domain.Post{
		UserID:    userID,
		Content:   req.Content,
		ImageURLs: req.ImageURLs,
	}

	if err := post.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	created, err := uc.postRepo.Create(ctx, post)
	if err != nil {
		return nil, errors.DatabaseError("failed to create post", err)
	}

	return created, nil
}

// DeletePostUseCase represents the use case for deleting a post
type DeletePostUseCase struct {
	postRepo repository.PostRepository
}

// NewDeletePostUseCase creates a new DeletePostUseCase
func NewDeletePostUseCase(postRepo repository.PostRepository) *DeletePostUseCase {
	return &DeletePostUseCase{postRepo: postRepo}
}

// Execute deletes a post
func (uc *DeletePostUseCase) Execute(ctx context.Context, postID int64, currentUserID int64) error {
	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return errors.DatabaseError("failed to get post", err)
	}

	if post == nil {
		return errors.NotFound("post not found")
	}

	if post.UserID != currentUserID {
		return errors.Forbidden("you can only delete your own posts")
	}

	if err := uc.postRepo.Delete(ctx, postID); err != nil {
		return errors.DatabaseError("failed to delete post", err)
	}

	return nil
}

// ListPostsUseCase represents the use case for listing posts
type ListPostsUseCase struct {
	postRepo repository.PostRepository
}

// NewListPostsUseCase creates a new ListPostsUseCase
func NewListPostsUseCase(postRepo repository.PostRepository) *ListPostsUseCase {
	return &ListPostsUseCase{postRepo: postRepo}
}

// Execute lists posts with pagination
func (uc *ListPostsUseCase) Execute(ctx context.Context, userID *int64, limit, offset int) ([]*domain.Post, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	posts, total, err := uc.postRepo.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, errors.DatabaseError("failed to list posts", err)
	}

	return posts, total, nil
}
