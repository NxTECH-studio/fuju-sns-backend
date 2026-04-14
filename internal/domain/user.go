package domain

import (
	"context"
	"fmt"
	"net/mail"
	"time"
)

// User represents a user entity
type User struct {
	ID            int64
	Username      string
	Email         string
	DisplayName   string
	Bio           string
	AvatarURL     string
	OAuthProvider string // e.g., "google", "github"
	OAuthID       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

// CreateUserRequest is used to create a new user
type CreateUserRequest struct {
	Username      string
	Email         string
	DisplayName   string
	Bio           string
	AvatarURL     string
	OAuthProvider string
	OAuthID       string
}

// UpdateUserRequest is used to update an existing user
type UpdateUserRequest struct {
	DisplayName *string
	Bio         *string
	AvatarURL   *string
}

// Validate validates user data
func (u *User) Validate() error {
	if len(u.Username) < 3 || len(u.Username) > 50 {
		return &InvalidUserError{Reason: "username must be between 3 and 50 characters"}
	}

	if _, err := mail.ParseAddress(u.Email); err != nil {
		return &InvalidUserError{Reason: fmt.Sprintf("invalid email: %s", u.Email)}
	}

	if len(u.DisplayName) > 255 {
		return &InvalidUserError{Reason: "display name must be less than 255 characters"}
	}

	if len(u.Bio) > 500 {
		return &InvalidUserError{Reason: "bio must be less than 500 characters"}
	}

	if len(u.AvatarURL) > 1024 {
		return &InvalidUserError{Reason: "avatar URL must be less than 1024 characters"}
	}

	if u.OAuthProvider == "" {
		return &InvalidUserError{Reason: "oauth provider is required"}
	}

	if u.OAuthID == "" {
		return &InvalidUserError{Reason: "oauth ID is required"}
	}

	return nil
}

// UserRepository defines methods for user persistence
type UserRepository interface {
	// Create creates a new user
	Create(ctx context.Context, user *User) (*User, error)

	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id int64) (*User, error)

	// GetByUsername retrieves a user by username
	GetByUsername(ctx context.Context, username string) (*User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (*User, error)

	// GetByOAuthID retrieves a user by OAuth provider and OAuth ID
	GetByOAuthID(ctx context.Context, provider, oauthID string) (*User, error)

	// Update updates an existing user
	Update(ctx context.Context, id int64, req *UpdateUserRequest) (*User, error)

	// Delete soft-deletes a user
	Delete(ctx context.Context, id int64) error

	// List lists users with pagination
	List(ctx context.Context, limit, offset int) ([]*User, int64, error)
}
