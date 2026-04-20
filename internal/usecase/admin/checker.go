// Package admin provides the admin privilege check that reads
// users.is_admin. A single boolean column is sufficient for MVP — a real RBAC
// layer is a later task.
package admin

import (
	"context"

	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
)

// Checker answers "is this sub an admin?" off the users mirror.
type Checker struct {
	userRepo repository.UserRepository
}

// NewChecker constructs an admin Checker.
func NewChecker(userRepo repository.UserRepository) *Checker {
	return &Checker{userRepo: userRepo}
}

// IsAdmin returns (true, nil) iff the user row exists and its is_admin flag
// is set. A missing row is "not admin" (false, nil).
func (c *Checker) IsAdmin(ctx context.Context, sub string) (bool, error) {
	user, err := c.userRepo.GetBySub(ctx, sub)
	if err != nil {
		return false, errors.DatabaseError("failed to load user for admin check", err)
	}
	if user == nil {
		return false, nil
	}
	return user.IsAdmin, nil
}
