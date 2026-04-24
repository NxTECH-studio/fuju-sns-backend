// Package domain contains domain models and errors.
package domain

import (
	"fmt"
)

// UserNotFoundError indicates user was not found.
type UserNotFoundError struct {
	Sub string
}

func (e *UserNotFoundError) Error() string {
	return fmt.Sprintf("user not found: sub=%s", e.Sub)
}

// PostNotFoundError indicates post was not found.
type PostNotFoundError struct {
	ID string
}

func (e *PostNotFoundError) Error() string {
	return fmt.Sprintf("post not found: id=%s", e.ID)
}

// InvalidUserError indicates user validation failed.
type InvalidUserError struct {
	Reason string
}

func (e *InvalidUserError) Error() string {
	return fmt.Sprintf("invalid user: %s", e.Reason)
}

// InvalidPostError indicates post validation failed.
type InvalidPostError struct {
	Reason string
}

func (e *InvalidPostError) Error() string {
	return fmt.Sprintf("invalid post: %s", e.Reason)
}

// AccessDeniedError indicates the caller lacks permission.
type AccessDeniedError struct {
	Resource string
	Sub      string
}

func (e *AccessDeniedError) Error() string {
	return fmt.Sprintf("access denied: sub=%s, resource=%s", e.Sub, e.Resource)
}

// ValidationError indicates data validation failed.
type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s", e.Reason)
}

// NewValidationError creates a new validation error.
func NewValidationError(reason string) error {
	return &ValidationError{Reason: reason}
}
