package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	apperrors "github.com/fuju/backend/pkg/errors"
)

// MockUserRepository is a mock implementation of UserRepository
type MockUserRepository struct {
	users map[int64]*domain.User
	err   error
}

// Create creates a new user in mock
func (m *MockUserRepository) Create(_ context.Context, user *domain.User) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.users[user.ID] = user
	return user, nil
}

// GetByID retrieves a user by ID from mock
func (m *MockUserRepository) GetByID(_ context.Context, id int64) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	user, ok := m.users[id]
	if !ok {
		return nil, &domain.UserNotFoundError{ID: id}
	}
	return user, nil
}

// GetByUsername retrieves a user by username from mock
func (m *MockUserRepository) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, user := range m.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, &domain.UserNotFoundError{}
}

// GetByEmail retrieves a user by email from mock
func (m *MockUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, user := range m.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, &domain.UserNotFoundError{}
}

// GetByOAuthID retrieves a user by OAuth ID from mock
func (m *MockUserRepository) GetByOAuthID(_ context.Context, provider, oauthID string) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, user := range m.users {
		if user.OAuthProvider == provider && user.OAuthID == oauthID {
			return user, nil
		}
	}
	return nil, &domain.UserNotFoundError{}
}

// Update updates a user in mock
func (m *MockUserRepository) Update(_ context.Context, id int64, req *domain.UpdateUserRequest) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	user, ok := m.users[id]
	if !ok {
		return nil, &domain.UserNotFoundError{ID: id}
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Bio != nil {
		user.Bio = *req.Bio
	}
	if req.AvatarURL != nil {
		user.AvatarURL = *req.AvatarURL
	}
	user.UpdatedAt = time.Now()
	return user, nil
}

// Delete soft-deletes a user in mock
func (m *MockUserRepository) Delete(_ context.Context, id int64) error {
	if m.err != nil {
		return m.err
	}
	user, ok := m.users[id]
	if !ok {
		return &domain.UserNotFoundError{ID: id}
	}
	now := time.Now()
	user.DeletedAt = &now
	return nil
}

// List lists users from mock
func (m *MockUserRepository) List(_ context.Context, limit, offset int) ([]*domain.User, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	var users []*domain.User
	for _, user := range m.users {
		users = append(users, user)
	}
	return users, int64(len(users)), nil
}

// TestGetUserUsecase_Success tests successful GetUser execution
func TestGetUserUsecase_Success(t *testing.T) {
	// Arrange
	mockRepo := &MockUserRepository{
		users: map[int64]*domain.User{
			1: {
				ID:            1,
				Username:      "johndoe",
				Email:         "john@example.com",
				DisplayName:   "John Doe",
				OAuthProvider: "google",
				OAuthID:       "123456",
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		},
	}

	usecase := NewGetUserUsecase(mockRepo)
	ctx := context.Background()

	// Act
	user, err := usecase.Execute(ctx, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user == nil {
		t.Fatal("expected user, got nil")
	}

	if user.ID != 1 {
		t.Errorf("expected user ID 1, got %d", user.ID)
	}

	if user.Username != "johndoe" {
		t.Errorf("expected username 'johndoe', got %s", user.Username)
	}
}

// TestGetUserUsecase_NotFound tests GetUser when user doesn't exist
func TestGetUserUsecase_NotFound(t *testing.T) {
	// Arrange
	mockRepo := &MockUserRepository{
		users: map[int64]*domain.User{},
	}

	usecase := NewGetUserUsecase(mockRepo)
	ctx := context.Background()

	// Act
	user, err := usecase.Execute(ctx, 999)

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if user != nil {
		t.Errorf("expected nil user, got %v", user)
	}

	// Check error type
	appErr, ok := apperrors.IsAppError(err)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}

	if appErr.Code != apperrors.ErrNotFound {
		t.Errorf("expected error code %s, got %s", apperrors.ErrNotFound, appErr.Code)
	}
}

// TestGetUserUsecase_InvalidID tests GetUser with invalid ID
func TestGetUserUsecase_InvalidID(t *testing.T) {
	tests := []struct {
		name   string
		userID int64
	}{
		{"zero ID", 0},
		{"negative ID", -1},
	}

	mockRepo := &MockUserRepository{
		users: map[int64]*domain.User{},
	}

	usecase := NewGetUserUsecase(mockRepo)
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := usecase.Execute(ctx, tt.userID)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if user != nil {
				t.Errorf("expected nil user, got %v", user)
			}

			appErr, ok := apperrors.IsAppError(err)
			if !ok {
				t.Fatalf("expected AppError, got %T", err)
			}

			if appErr.Code != apperrors.ErrInvalidRequest {
				t.Errorf("expected error code %s, got %s", apperrors.ErrInvalidRequest, appErr.Code)
			}
		})
	}
}

// TestGetUserUsecase_RepositoryError tests GetUser when repository returns error
func TestGetUserUsecase_RepositoryError(t *testing.T) {
	// Arrange
	mockRepo := &MockUserRepository{
		users: map[int64]*domain.User{},
		err:   errors.New("database connection failed"),
	}

	usecase := NewGetUserUsecase(mockRepo)
	ctx := context.Background()

	// Act
	user, err := usecase.Execute(ctx, 1)

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if user != nil {
		t.Errorf("expected nil user, got %v", user)
	}

	// Check error type
	appErr, ok := apperrors.IsAppError(err)
	if !ok {
		t.Fatalf("expected AppError, got %T", err)
	}

	if appErr.Code != apperrors.ErrDatabaseError {
		t.Errorf("expected error code %s, got %s", apperrors.ErrDatabaseError, appErr.Code)
	}
}
