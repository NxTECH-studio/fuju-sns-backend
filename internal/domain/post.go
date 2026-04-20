package domain

import (
	"time"
)

// Post is a user-authored post. ID and UserID are ULIDs. This is a minimal
// shape; richer fields (replies, likes, etc.) land in the post feature task.
type Post struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	Content       string     `json:"content"`
	ImageURLs     []string   `json:"image_urls,omitempty"`
	LikesCount    int64      `json:"likes_count"`
	CommentsCount int64      `json:"comments_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// CreatePostRequest represents a request to create a post.
type CreatePostRequest struct {
	Content   string   `json:"content"`
	ImageURLs []string `json:"image_urls,omitempty"`
}

// Validate validates the Post.
func (p *Post) Validate() error {
	if len(p.Content) == 0 || len(p.Content) > 5000 {
		return NewValidationError("content must be between 1 and 5000 characters")
	}
	if len(p.ImageURLs) > 10 {
		return NewValidationError("maximum 10 images allowed")
	}
	if p.UserID == "" {
		return NewValidationError("user_id is required")
	}
	return nil
}
