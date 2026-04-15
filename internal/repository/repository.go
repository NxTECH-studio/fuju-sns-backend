// Package repository defines repository interfaces.
package repository

import (
	"context"

	"github.com/fuju/backend/internal/domain"
)

// UserRepository defines user persistence operations
type UserRepository interface {
	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id int64) (*domain.User, error)

	// GetByUsername retrieves a user by username
	GetByUsername(ctx context.Context, username string) (*domain.User, error)

	// GetByOAuthID retrieves a user by OAuth provider and ID
	GetByOAuthID(ctx context.Context, provider, oauthID string) (*domain.User, error)

	// List retrieves a paginated list of users
	List(ctx context.Context, limit, offset int) ([]*domain.User, int, error)

	// Create creates a new user
	Create(ctx context.Context, user *domain.User) (*domain.User, error)

	// Update updates an existing user
	Update(ctx context.Context, user *domain.User) (*domain.User, error)

	// Delete soft-deletes a user
	Delete(ctx context.Context, id int64) error
}

// PostRepository defines post persistence operations
type PostRepository interface {
	// GetByID retrieves a post by ID with user details
	GetByID(ctx context.Context, id int64) (*domain.Post, error)

	// List retrieves a paginated list of posts with optional user filter
	List(ctx context.Context, userID *int64, limit, offset int) ([]*domain.Post, int, error)

	// Create creates a new post
	Create(ctx context.Context, post *domain.Post) (*domain.Post, error)

	// Delete soft-deletes a post
	Delete(ctx context.Context, id int64) error

	// IncrementCommentCount increments the comment count for a post
	IncrementCommentCount(ctx context.Context, postID int64) error

	// DecrementCommentCount decrements the comment count for a post
	DecrementCommentCount(ctx context.Context, postID int64) error
}

// CommentRepository defines comment persistence operations
type CommentRepository interface {
	// GetByID retrieves a comment by ID with user details
	GetByID(ctx context.Context, id int64) (*domain.Comment, error)

	// ListByPostID retrieves comments for a post
	ListByPostID(ctx context.Context, postID int64, limit, offset int) ([]*domain.Comment, int, error)

	// Create creates a new comment
	Create(ctx context.Context, comment *domain.Comment) (*domain.Comment, error)

	// Delete soft-deletes a comment
	Delete(ctx context.Context, id int64) error
}

// ImageRepository defines image persistence operations
type ImageRepository interface {
	// GetByID retrieves an image by ID
	GetByID(ctx context.Context, id string) (*domain.Image, error)

	// GetByUserID retrieves all images for a user
	GetByUserID(ctx context.Context, userID int64) ([]*domain.Image, error)

	// Create stores a new image record
	Create(ctx context.Context, image *domain.Image) (*domain.Image, error)

	// Delete soft-deletes an image
	Delete(ctx context.Context, id string) error
}
