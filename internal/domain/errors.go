// Package domain contains domain models and errors.
package domain

import (
	"fmt"
)

// Domain-specific errors

// UserNotFoundError indicates user was not found
type UserNotFoundError struct {
	ID int64
}

func (e *UserNotFoundError) Error() string {
	return fmt.Sprintf("user not found: id=%d", e.ID)
}

// PostNotFoundError indicates post was not found
type PostNotFoundError struct {
	ID int64
}

func (e *PostNotFoundError) Error() string {
	return fmt.Sprintf("post not found: id=%d", e.ID)
}

// CommentNotFoundError indicates comment was not found
type CommentNotFoundError struct {
	ID int64
}

func (e *CommentNotFoundError) Error() string {
	return fmt.Sprintf("comment not found: id=%d", e.ID)
}

// InvalidUserError indicates user validation failed
type InvalidUserError struct {
	Reason string
}

func (e *InvalidUserError) Error() string {
	return fmt.Sprintf("invalid user: %s", e.Reason)
}

// InvalidPostError indicates post validation failed
type InvalidPostError struct {
	Reason string
}

func (e *InvalidPostError) Error() string {
	return fmt.Sprintf("invalid post: %s", e.Reason)
}

// InvalidCommentError indicates comment validation failed
type InvalidCommentError struct {
	Reason string
}

func (e *InvalidCommentError) Error() string {
	return fmt.Sprintf("invalid comment: %s", e.Reason)
}

// AccessDeniedError indicates user does not have permission
type AccessDeniedError struct {
	Resource string
	UserID   int64
}

func (e *AccessDeniedError) Error() string {
	return fmt.Sprintf("access denied: user=%d, resource=%s", e.UserID, e.Resource)
}

// DuplicateUsernameError indicates username already exists
type DuplicateUsernameError struct {
	Username string
}

func (e *DuplicateUsernameError) Error() string {
	return fmt.Sprintf("duplicate username: %s", e.Username)
}

// DuplicateEmailError indicates email already exists
type DuplicateEmailError struct {
	Email string
}

func (e *DuplicateEmailError) Error() string {
	return fmt.Sprintf("duplicate email: %s", e.Email)
}
