package domain

import (
	"time"
)

// Comment represents a comment on a post
type Comment struct {
	ID        int64      `json:"id"`
	PostID    int64      `json:"post_id"`
	UserID    int64      `json:"user_id"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// CreateCommentRequest represents a request to create a comment
type CreateCommentRequest struct {
	Content string `json:"content"`
}

// Validate validates the Comment
func (c *Comment) Validate() error {
	if len(c.Content) == 0 || len(c.Content) > 1000 {
		return NewValidationError("content must be between 1 and 1000 characters")
	}
	if c.PostID == 0 {
		return NewValidationError("post_id is required")
	}
	if c.UserID == 0 {
		return NewValidationError("user_id is required")
	}
	return nil
}
