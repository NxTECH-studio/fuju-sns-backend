// Package user provides user-related use cases.
package user

import (
	"context"
	"fmt"
	"net/http"

	"github.com/fuju/backend/internal/domain"
	apperrors "github.com/fuju/backend/pkg/errors"
)

// GetUserUsecase retrieves a user by ID
type GetUserUsecase struct {
	repository domain.UserRepository
}

// NewGetUserUsecase creates a new GetUserUsecase
func NewGetUserUsecase(repository domain.UserRepository) *GetUserUsecase {
	return &GetUserUsecase{
		repository: repository,
	}
}

// Execute retrieves a user by ID
func (u *GetUserUsecase) Execute(ctx context.Context, id int64) (*domain.User, error) {
	if id <= 0 {
		return nil, apperrors.InvalidRequest("user ID must be positive", fmt.Errorf("invalid user ID: %d", id))
	}

	user, err := u.repository.GetByID(ctx, id)
	if err != nil {
		if _, ok := err.(*domain.UserNotFoundError); ok {
			return nil, apperrors.NewWithStatus(
				apperrors.ErrNotFound,
				fmt.Sprintf("user not found: id=%d", id),
				http.StatusNotFound,
			)
		}
		return nil, apperrors.DatabaseError(
			fmt.Sprintf("failed to get user: id=%d", id),
			err,
		)
	}

	return user, nil
}
