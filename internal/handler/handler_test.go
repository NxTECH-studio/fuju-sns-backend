// Package handler provides HTTP request handlers.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fuju/backend/internal/domain"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/response"
)

// TestHealthHandlerResponse tests basic health endpoint response structure
func TestHealthHandlerResponse(t *testing.T) {
	// Arrange
	handler := NewHealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act
	handler.Health(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var resp response.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp.Status)
	}

	if resp.Timestamp == "" {
		t.Error("expected timestamp in response, got empty")
	}
}

// TestWriteSuccessResponseFormat tests success response has correct JSON structure
func TestWriteSuccessResponseFormat(t *testing.T) {
	// Arrange
	w := httptest.NewRecorder()
	testData := map[string]interface{}{"id": 1, "name": "test"}

	// Act
	WriteSuccessResponse(w, testData, http.StatusOK)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var resp response.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Data == nil {
		t.Error("expected data in response")
	}
}

// TestWriteErrorResponseFormat tests error response has correct structure
func TestWriteErrorResponseFormat(t *testing.T) {
	// Arrange
	w := httptest.NewRecorder()
	testErr := &domain.UserNotFoundError{ID: 1}

	// Act
	WriteErrorResponse(w, testErr)

	// Assert
	if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var resp response.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Code == "" {
		t.Error("expected error code in response")
	}

	if resp.Message == "" {
		t.Error("expected error message in response")
	}
}

// TestWriteListResponseFormat tests list response has correct pagination fields
func TestWriteListResponseFormat(t *testing.T) {
	// Arrange
	w := httptest.NewRecorder()
	listResp := &response.ListResponse{
		Data:   []interface{}{},
		Limit:  20,
		Offset: 0,
		Total:  0,
	}

	// Act
	WriteListResponse(w, listResp, http.StatusOK)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var resp response.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Limit == 0 {
		t.Error("expected limit in response")
	}

	if resp.Offset < 0 {
		t.Error("expected valid offset in response")
	}
}

// MockUserRepository is a mock for UserRepository
type MockUserRepository struct {
	users map[int64]*domain.User
	err   error
}

func (m *MockUserRepository) Create(_ context.Context, user *domain.User) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.users[user.ID] = user
	return user, nil
}

func (m *MockUserRepository) GetByID(_ context.Context, id int64) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	user, ok := m.users[id]
	if !ok {
		return nil, nil
	}
	return user, nil
}

func (m *MockUserRepository) GetByUsername(_ context.Context, _ string) (*domain.User, error) {
	return nil, nil
}

func (m *MockUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, user := range m.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, &domain.UserNotFoundError{ID: 0}
}

func (m *MockUserRepository) GetByOAuthID(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, nil
}

func (m *MockUserRepository) Update(_ context.Context, user *domain.User) (*domain.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.users[user.ID] = user
	return user, nil
}

func (m *MockUserRepository) Delete(_ context.Context, id int64) error {
	if m.err != nil {
		return m.err
	}
	delete(m.users, id)
	return nil
}

func (m *MockUserRepository) List(_ context.Context, _, _ int) ([]*domain.User, int, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	var users []*domain.User
	for _, user := range m.users {
		users = append(users, user)
	}
	return users, len(users), nil
}

// TestHealthHandler tests the health check endpoint
func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Health check returns OK",
			expectedStatus: http.StatusOK,
			expectedBody:   "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			handler := NewHealthHandler()
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			// Act
			handler.Health(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != ContentTypeJSON {
				t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
			}

			var resp response.HealthResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if resp.Status != tt.expectedBody {
				t.Errorf("expected status %s, got %s", tt.expectedBody, resp.Status)
			}
		})
	}
}

// TestGetUserHandler tests the GET user endpoint
func TestGetUserHandler(t *testing.T) {
	tests := []struct {
		name           string
		userID         int64
		users          map[int64]*domain.User
		expectedStatus int
		expectedError  bool
	}{
		{
			name:           "Get existing user",
			userID:         1,
			users:          map[int64]*domain.User{1: {ID: 1, Username: "testuser", Email: "test@example.com"}},
			expectedStatus: http.StatusOK,
			expectedError:  false,
		},
		{
			name:           "Get non-existent user",
			userID:         999,
			users:          map[int64]*domain.User{},
			expectedStatus: http.StatusNotFound,
			expectedError:  true,
		},
		{
			name:           "Invalid user ID",
			userID:         0,
			users:          map[int64]*domain.User{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			mockRepo := &MockUserRepository{users: tt.users}
			getUsecase := userusecase.NewGetUserUseCase(mockRepo)
			h := NewUserHandler(getUsecase, nil, nil, nil)
			userIDStr := strconv.FormatInt(tt.userID, 10)
			req := httptest.NewRequest("GET", "/users/"+userIDStr, nil)
			req.SetPathValue("id", userIDStr)
			w := httptest.NewRecorder()

			// Act
			h.GetUser(w, req)

			// Assert
			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
				t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
			}
		})
	}
}

// TestHealthHandlerMeta tests that health response includes metadata
func TestHealthHandlerMeta(t *testing.T) {
	// Arrange
	handler := NewHealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Act
	handler.Health(w, req)

	// Assert
	var resp response.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Timestamp == "" {
		t.Error("expected timestamp in response, got empty string")
	}
}

// TestWriteErrorResponse tests error response writing
func TestWriteErrorResponse(t *testing.T) {
	// Arrange
	w := httptest.NewRecorder()
	testErr := &domain.UserNotFoundError{ID: 1}

	// Act
	WriteErrorResponse(w, testErr)

	// Assert
	if contentType := w.Header().Get("Content-Type"); contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var resp response.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Code == "" {
		t.Error("expected error code in response")
	}
}
