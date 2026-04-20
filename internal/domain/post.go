package domain

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// MaxContentLen is the rune-count upper bound on Post.Content. Kept in sync
// with the CHECK constraint in migration 004 and the app-layer validator.
const MaxContentLen = 120

// MaxImagesPerPost caps how many images may be attached to one post (X
// parity). The DB `post_images.position` CHECK enforces the same bound.
const MaxImagesPerPost = 4

// Post is a user-authored post. Replies are represented by a non-nil
// ParentPostID pointing at another Post — the old Comment domain has been
// collapsed into this model.
type Post struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	Content      string     `json:"content"`
	ParentPostID *string    `json:"parent_post_id,omitempty"`
	RootPostID   *string    `json:"root_post_id,omitempty"`
	LikesCount   int64      `json:"likes_count"`
	RepliesCount int64      `json:"replies_count"`
	Visibility   string     `json:"visibility"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

// CreatePostRequest is the POST /v1/posts payload. ImageIDs refer to
// already-uploaded Image records owned by the caller.
type CreatePostRequest struct {
	Content      string   `json:"content"`
	ImageIDs     []string `json:"image_ids,omitempty"`
	ParentPostID *string  `json:"parent_post_id,omitempty"`
}

// Validate checks the domain-level invariants that don't depend on
// repository state. Ownership / existence checks live in the use case.
func (p *Post) Validate() error {
	if p.Content == "" || utf8.RuneCountInString(p.Content) > MaxContentLen {
		return NewValidationError(fmt.Sprintf("content must be 1..%d chars", MaxContentLen))
	}
	if p.UserID == "" {
		return NewValidationError("user_id is required")
	}
	if p.ParentPostID != nil && *p.ParentPostID == "" {
		return NewValidationError("parent_post_id must be non-empty if present")
	}
	return nil
}

// Validate checks request-level invariants. Image count is bounded here
// because domain.Post has no direct image field — the usecase pairs the
// Post with ImageIDs separately.
func (r *CreatePostRequest) Validate() error {
	if r.Content == "" || utf8.RuneCountInString(r.Content) > MaxContentLen {
		return NewValidationError(fmt.Sprintf("content must be 1..%d chars", MaxContentLen))
	}
	if len(r.ImageIDs) > MaxImagesPerPost {
		return NewValidationError(fmt.Sprintf("at most %d images per post", MaxImagesPerPost))
	}
	if r.ParentPostID != nil && *r.ParentPostID == "" {
		return NewValidationError("parent_post_id must be non-empty if present")
	}
	return nil
}
