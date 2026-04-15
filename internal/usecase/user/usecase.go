// Package user contains user business logic use cases.
package user

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
)

// GetUserUseCase represents the use case for getting a user
type GetUserUseCase struct {
	userRepo repository.UserRepository
}

// NewGetUserUseCase creates a new GetUserUseCase
func NewGetUserUseCase(userRepo repository.UserRepository) *GetUserUseCase {
	return &GetUserUseCase{userRepo: userRepo}
}

// Execute retrieves a user by ID
func (uc *GetUserUseCase) Execute(ctx context.Context, userID int64) (*domain.User, error) {
	if userID <= 0 {
		return nil, errors.InvalidRequest("invalid user ID", nil)
	}

	user, err := uc.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.DatabaseError("failed to get user", err)
	}

	if user == nil {
		return nil, errors.NotFound("user not found")
	}

	return user, nil
}

// CreateUserUseCase represents the use case for creating a user
type CreateUserUseCase struct {
	userRepo repository.UserRepository
}

// NewCreateUserUseCase creates a new CreateUserUseCase
func NewCreateUserUseCase(userRepo repository.UserRepository) *CreateUserUseCase {
	return &CreateUserUseCase{userRepo: userRepo}
}

// Execute creates a new user
func (uc *CreateUserUseCase) Execute(ctx context.Context, req *domain.CreateUserRequest) (*domain.User, error) {
	// Check if username already exists
	existing, err := uc.userRepo.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, errors.DatabaseError("failed to check username", err)
	}

	if existing != nil {
		return nil, errors.Conflict("username already exists")
	}

	user := &domain.User{
		Username:      req.Username,
		Email:         req.Email,
		DisplayName:   req.DisplayName,
		Bio:           req.Bio,
		AvatarURL:     req.AvatarURL,
		OAuthProvider: req.OAuthProvider,
		OAuthID:       req.OAuthID,
	}

	if err := user.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	created, err := uc.userRepo.Create(ctx, user)
	if err != nil {
		return nil, errors.DatabaseError("failed to create user", err)
	}

	return created, nil
}

// UpdateUserUseCase represents the use case for updating a user
type UpdateUserUseCase struct {
	userRepo repository.UserRepository
}

// NewUpdateUserUseCase creates a new UpdateUserUseCase
func NewUpdateUserUseCase(userRepo repository.UserRepository) *UpdateUserUseCase {
	return &UpdateUserUseCase{userRepo: userRepo}
}

// Execute updates a user
func (uc *UpdateUserUseCase) Execute(ctx context.Context, userID int64, currentUserID int64, req *domain.UpdateUserRequest) (*domain.User, error) {
	// Authorization check - user can only update their own profile
	if userID != currentUserID {
		return nil, errors.Forbidden("you can only update your own profile")
	}

	user, err := uc.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.DatabaseError("failed to get user", err)
	}

	if user == nil {
		return nil, errors.NotFound("user not found")
	}

	// Apply partial updates
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.AvatarURL != nil {
		user.AvatarURL = *req.AvatarURL
	}

	if err := user.Validate(); err != nil {
		return nil, errors.ValidationFailed(err.Error())
	}

	updated, err := uc.userRepo.Update(ctx, user)
	if err != nil {
		return nil, errors.DatabaseError("failed to update user", err)
	}

	return updated, nil
}

// ListUsersUseCase represents the use case for listing users
type ListUsersUseCase struct {
	userRepo repository.UserRepository
}

// NewListUsersUseCase creates a new ListUsersUseCase
func NewListUsersUseCase(userRepo repository.UserRepository) *ListUsersUseCase {
	return &ListUsersUseCase{userRepo: userRepo}
}

// Execute lists users with pagination
func (uc *ListUsersUseCase) Execute(ctx context.Context, limit, offset int) ([]*domain.User, int, error) {
	// Validate pagination parameters
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	users, total, err := uc.userRepo.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, errors.DatabaseError("failed to list users", err)
	}

	return users, total, nil
}
