// Package comment contains comment business logic use cases.
package comment

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
	"github.com/oklog/ulid/v2"
)

// AddCommentUseCase represents the use case for adding a comment.
type AddCommentUseCase struct {
	commentRepo repository.CommentRepository
	postRepo    repository.PostRepository
}

// NewAddCommentUseCase creates a new AddCommentUseCase.
func NewAddCommentUseCase(
	commentRepo repository.CommentRepository,
	postRepo repository.PostRepository,
) *AddCommentUseCase {
	return &AddCommentUseCase{
		commentRepo: commentRepo,
		postRepo:    postRepo,
	}
}

// Execute adds a comment to a post.
func (uc *AddCommentUseCase) Execute(ctx context.Context, postID, userSub string, req *domain.CreateCommentRequest) (*domain.Comment, error) {
	post, err := uc.postRepo.GetByID(ctx, postID)
	if err != nil {
		return nil, errors.DatabaseError("failed to get post", err)
	}

	if post == nil {
		return nil, errors.NotFound("post not found")
	}

	comment := &domain.Comment{
		ID:      ulid.Make().String(),
		PostID:  postID,
		UserID:  userSub,
		Content: req.Content,
	}

	if err := comment.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	created, err := uc.commentRepo.Create(ctx, comment)
	if err != nil {
		return nil, errors.DatabaseError("failed to create comment", err)
	}

	if err := uc.postRepo.IncrementCommentCount(ctx, postID); err != nil {
		return nil, errors.DatabaseError("failed to update post comment count", err)
	}

	return created, nil
}

// DeleteCommentUseCase represents the use case for deleting a comment.
type DeleteCommentUseCase struct {
	commentRepo repository.CommentRepository
	postRepo    repository.PostRepository
}

// NewDeleteCommentUseCase creates a new DeleteCommentUseCase.
func NewDeleteCommentUseCase(
	commentRepo repository.CommentRepository,
	postRepo repository.PostRepository,
) *DeleteCommentUseCase {
	return &DeleteCommentUseCase{
		commentRepo: commentRepo,
		postRepo:    postRepo,
	}
}

// Execute deletes a comment.
func (uc *DeleteCommentUseCase) Execute(ctx context.Context, commentID, currentUserSub string) error {
	comment, err := uc.commentRepo.GetByID(ctx, commentID)
	if err != nil {
		return errors.DatabaseError("failed to get comment", err)
	}

	if comment == nil {
		return errors.NotFound("comment not found")
	}

	if comment.UserID != currentUserSub {
		return errors.Forbidden("you can only delete your own comments")
	}

	if err := uc.commentRepo.Delete(ctx, commentID); err != nil {
		return errors.DatabaseError("failed to delete comment", err)
	}

	if err := uc.postRepo.DecrementCommentCount(ctx, comment.PostID); err != nil {
		return errors.DatabaseError("failed to update post comment count", err)
	}

	return nil
}
