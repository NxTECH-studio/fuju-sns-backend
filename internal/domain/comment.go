package domain

import (
	"time"
)

// Comment is a user comment on a post. Retained transitionally; the post
// feature task collapses comments into post replies.
type Comment struct {
	ID        string     `json:"id"`
	PostID    string     `json:"post_id"`
	UserID    string     `json:"user_id"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// CreateCommentRequest represents a request to create a comment.
type CreateCommentRequest struct {
	Content string `json:"content"`
}

// Validate validates the Comment.
func (c *Comment) Validate() error {
	if len(c.Content) == 0 || len(c.Content) > 1000 {
		return NewValidationError("content must be between 1 and 1000 characters")
	}
	if c.PostID == "" {
		return NewValidationError("post_id is required")
	}
	if c.UserID == "" {
		return NewValidationError("user_id is required")
	}
	return nil
}
