// Package inmemory provides in-memory implementations of repositories for testing.
package inmemory

import (
	"context"
	"sync"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
)

// UserRepository is an in-memory implementation of the UserRepository interface.
type UserRepository struct {
	mu    sync.RWMutex
	users map[string]*domain.User
}

// NewUserRepository creates a new in-memory user repository.
func NewUserRepository() repository.UserRepository {
	return &UserRepository{
		users: make(map[string]*domain.User),
	}
}

// GetBySub retrieves a user by AuthCore sub.
func (r *UserRepository) GetBySub(_ context.Context, sub string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[sub]
	if !ok {
		return nil, nil
	}

	userCopy := *user
	return &userCopy, nil
}

// Upsert inserts or updates a user. On update, the stored values for
// fields the caller did not populate (zero values in the input) are
// preserved — so hydrate refreshing only *_Cached fields cannot accidentally
// clobber is_admin, bio, banner_url, or created_at.
func (r *UserRepository) Upsert(_ context.Context, user *domain.User) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	existing, ok := r.users[user.Sub]
	stored := *user

	if ok {
		// Preserved server-owned fields.
		stored.IsAdmin = existing.IsAdmin
		stored.CreatedAt = existing.CreatedAt
		stored.DeletedAt = existing.DeletedAt
		// Preserve SNS-owned fields when the caller did not overwrite them.
		if stored.Bio == "" {
			stored.Bio = existing.Bio
		}
		if stored.BannerURL == "" {
			stored.BannerURL = existing.BannerURL
		}
	} else if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now

	r.users[user.Sub] = &stored
	storedCopy := stored
	return &storedCopy, nil
}

// UpdateProfile updates SNS-owned fields (bio, banner_url).
func (r *UserRepository) UpdateProfile(_ context.Context, sub string, req *domain.UpdateUserProfileRequest) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[sub]
	if !ok {
		return nil, nil
	}

	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.BannerURL != nil {
		user.BannerURL = *req.BannerURL
	}
	user.UpdatedAt = time.Now()

	userCopy := *user
	return &userCopy, nil
}

// List retrieves a paginated list of users.
func (r *UserRepository) List(_ context.Context, limit, offset int) ([]*domain.User, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var users []*domain.User
	for _, user := range r.users {
		if user.DeletedAt == nil {
			userCopy := *user
			users = append(users, &userCopy)
		}
	}

	total := len(users)
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return users[offset:end], total, nil
}

// Delete soft-deletes a user.
func (r *UserRepository) Delete(_ context.Context, sub string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[sub]; ok {
		now := time.Now()
		user.DeletedAt = &now
	}

	return nil
}

// PostRepository is an in-memory implementation of the PostRepository interface.
type PostRepository struct {
	mu    sync.RWMutex
	posts map[string]*domain.Post
}

// NewPostRepository creates a new in-memory post repository.
func NewPostRepository() repository.PostRepository {
	return &PostRepository{
		posts: make(map[string]*domain.Post),
	}
}

// GetByID retrieves a post by ID.
func (r *PostRepository) GetByID(_ context.Context, id string) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	post, ok := r.posts[id]
	if !ok || post.DeletedAt != nil {
		return nil, nil
	}

	postCopy := *post
	return &postCopy, nil
}

// List retrieves a paginated list of posts.
func (r *PostRepository) List(_ context.Context, userID *string, limit, offset int) ([]*domain.Post, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var posts []*domain.Post
	for _, post := range r.posts {
		if post.DeletedAt == nil {
			if userID == nil || post.UserID == *userID {
				postCopy := *post
				posts = append(posts, &postCopy)
			}
		}
	}

	total := len(posts)
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return posts[offset:end], total, nil
}

// Create creates a new post. Caller is responsible for setting post.ID
// (typically a ULID).
func (r *PostRepository) Create(_ context.Context, post *domain.Post) (*domain.Post, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	post.CreatedAt = now
	post.UpdatedAt = now

	postCopy := *post
	r.posts[post.ID] = &postCopy
	return &postCopy, nil
}

// Delete soft-deletes a post.
func (r *PostRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[id]; ok {
		now := time.Now()
		post.DeletedAt = &now
	}

	return nil
}

// IncrementCommentCount increments the comment count.
func (r *PostRepository) IncrementCommentCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[postID]; ok {
		post.CommentsCount++
	}

	return nil
}

// DecrementCommentCount decrements the comment count.
func (r *PostRepository) DecrementCommentCount(_ context.Context, postID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[postID]; ok && post.CommentsCount > 0 {
		post.CommentsCount--
	}

	return nil
}

// CommentRepository is an in-memory implementation of the CommentRepository interface.
type CommentRepository struct {
	mu       sync.RWMutex
	comments map[string]*domain.Comment
}

// NewCommentRepository creates a new in-memory comment repository.
func NewCommentRepository() repository.CommentRepository {
	return &CommentRepository{
		comments: make(map[string]*domain.Comment),
	}
}

// GetByID retrieves a comment by ID.
func (r *CommentRepository) GetByID(_ context.Context, id string) (*domain.Comment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	comment, ok := r.comments[id]
	if !ok || comment.DeletedAt != nil {
		return nil, nil
	}

	commentCopy := *comment
	return &commentCopy, nil
}

// ListByPostID retrieves comments for a post.
func (r *CommentRepository) ListByPostID(_ context.Context, postID string, limit, offset int) ([]*domain.Comment, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var comments []*domain.Comment
	for _, comment := range r.comments {
		if comment.PostID == postID && comment.DeletedAt == nil {
			commentCopy := *comment
			comments = append(comments, &commentCopy)
		}
	}

	total := len(comments)
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return comments[offset:end], total, nil
}

// Create creates a new comment. Caller is responsible for setting comment.ID.
func (r *CommentRepository) Create(_ context.Context, comment *domain.Comment) (*domain.Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	comment.CreatedAt = now
	comment.UpdatedAt = now

	commentCopy := *comment
	r.comments[comment.ID] = &commentCopy
	return &commentCopy, nil
}

// Delete soft-deletes a comment.
func (r *CommentRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if comment, ok := r.comments[id]; ok {
		now := time.Now()
		comment.DeletedAt = &now
	}

	return nil
}

// ImageRepository is an in-memory implementation of the ImageRepository interface.
type ImageRepository struct {
	mu     sync.RWMutex
	images map[string]*domain.Image
}

// NewImageRepository creates a new in-memory image repository.
func NewImageRepository() repository.ImageRepository {
	return &ImageRepository{
		images: make(map[string]*domain.Image),
	}
}

// GetByID retrieves an image by ID.
func (r *ImageRepository) GetByID(_ context.Context, id string) (*domain.Image, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	image, ok := r.images[id]
	if !ok || image.DeletedAt != nil {
		return nil, nil
	}

	imageCopy := *image
	return &imageCopy, nil
}

// GetByUserID retrieves all images for a user.
func (r *ImageRepository) GetByUserID(_ context.Context, userID string) ([]*domain.Image, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var images []*domain.Image
	for _, image := range r.images {
		if image.UserID == userID && image.DeletedAt == nil {
			imageCopy := *image
			images = append(images, &imageCopy)
		}
	}

	return images, nil
}

// Create stores a new image record.
func (r *ImageRepository) Create(_ context.Context, image *domain.Image) (*domain.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	image.CreatedAt = now
	image.UpdatedAt = now

	imageCopy := *image
	r.images[image.ID] = &imageCopy
	return &imageCopy, nil
}

// Delete soft-deletes an image.
func (r *ImageRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if image, ok := r.images[id]; ok {
		now := time.Now()
		image.DeletedAt = &now
	}

	return nil
}
