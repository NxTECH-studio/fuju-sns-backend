// Package inmemory provides in-memory implementations of repositories for testing.
package inmemory

import (
	"context"
	"sync"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
)

// UserRepository is an in-memory implementation of the UserRepository interface
type UserRepository struct {
	mu    sync.RWMutex
	users map[int64]*domain.User
	idSeq int64
}

// NewUserRepository creates a new in-memory user repository
func NewUserRepository() repository.UserRepository {
	return &UserRepository{
		users: make(map[int64]*domain.User),
		idSeq: 0,
	}
}

// GetByID retrieves a user by ID
func (r *UserRepository) GetByID(_ context.Context, id int64) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return nil, nil
	}

	userCopy := *user
	return &userCopy, nil
}

// GetByUsername retrieves a user by username
func (r *UserRepository) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if user.Username == username {
			userCopy := *user
			return &userCopy, nil
		}
	}

	return nil, nil
}

// GetByOAuthID retrieves a user by OAuth provider and ID
func (r *UserRepository) GetByOAuthID(_ context.Context, provider, oauthID string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if user.OAuthProvider == provider && user.OAuthID == oauthID {
			userCopy := *user
			return &userCopy, nil
		}
	}

	return nil, nil
}

// List retrieves a paginated list of users
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

// Create creates a new user
func (r *UserRepository) Create(_ context.Context, user *domain.User) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.idSeq++
	user.ID = r.idSeq
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	userCopy := *user
	r.users[user.ID] = &userCopy
	return &userCopy, nil
}

// Update updates an existing user
func (r *UserRepository) Update(_ context.Context, user *domain.User) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[user.ID]; !ok {
		return nil, nil
	}

	user.UpdatedAt = time.Now()
	userCopy := *user
	r.users[user.ID] = &userCopy
	return &userCopy, nil
}

// Delete soft-deletes a user
func (r *UserRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[id]; ok {
		now := time.Now()
		user.DeletedAt = &now
	}

	return nil
}

// PostRepository is an in-memory implementation of the PostRepository interface
type PostRepository struct {
	mu    sync.RWMutex
	posts map[int64]*domain.Post
	idSeq int64
}

// NewPostRepository creates a new in-memory post repository
func NewPostRepository() repository.PostRepository {
	return &PostRepository{
		posts: make(map[int64]*domain.Post),
		idSeq: 0,
	}
}

// GetByID retrieves a post by ID
func (r *PostRepository) GetByID(_ context.Context, id int64) (*domain.Post, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	post, ok := r.posts[id]
	if !ok || post.DeletedAt != nil {
		return nil, nil
	}

	postCopy := *post
	return &postCopy, nil
}

// List retrieves a paginated list of posts
func (r *PostRepository) List(_ context.Context, userID *int64, limit, offset int) ([]*domain.Post, int, error) {
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

// Create creates a new post
func (r *PostRepository) Create(_ context.Context, post *domain.Post) (*domain.Post, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.idSeq++
	post.ID = r.idSeq
	post.CreatedAt = time.Now()
	post.UpdatedAt = time.Now()

	postCopy := *post
	r.posts[post.ID] = &postCopy
	return &postCopy, nil
}

// Delete soft-deletes a post
func (r *PostRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[id]; ok {
		now := time.Now()
		post.DeletedAt = &now
	}

	return nil
}

// IncrementCommentCount increments the comment count
func (r *PostRepository) IncrementCommentCount(_ context.Context, postID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[postID]; ok {
		post.CommentsCount++
	}

	return nil
}

// DecrementCommentCount decrements the comment count
func (r *PostRepository) DecrementCommentCount(_ context.Context, postID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if post, ok := r.posts[postID]; ok && post.CommentsCount > 0 {
		post.CommentsCount--
	}

	return nil
}

// CommentRepository is an in-memory implementation of the CommentRepository interface
type CommentRepository struct {
	mu       sync.RWMutex
	comments map[int64]*domain.Comment
	idSeq    int64
}

// NewCommentRepository creates a new in-memory comment repository
func NewCommentRepository() repository.CommentRepository {
	return &CommentRepository{
		comments: make(map[int64]*domain.Comment),
		idSeq:    0,
	}
}

// GetByID retrieves a comment by ID
func (r *CommentRepository) GetByID(_ context.Context, id int64) (*domain.Comment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	comment, ok := r.comments[id]
	if !ok || comment.DeletedAt != nil {
		return nil, nil
	}

	commentCopy := *comment
	return &commentCopy, nil
}

// ListByPostID retrieves comments for a post
func (r *CommentRepository) ListByPostID(_ context.Context, postID int64, limit, offset int) ([]*domain.Comment, int, error) {
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

// Create creates a new comment
func (r *CommentRepository) Create(_ context.Context, comment *domain.Comment) (*domain.Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.idSeq++
	comment.ID = r.idSeq
	comment.CreatedAt = time.Now()
	comment.UpdatedAt = time.Now()

	commentCopy := *comment
	r.comments[comment.ID] = &commentCopy
	return &commentCopy, nil
}

// Delete soft-deletes a comment
func (r *CommentRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if comment, ok := r.comments[id]; ok {
		now := time.Now()
		comment.DeletedAt = &now
	}

	return nil
}
