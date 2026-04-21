// Package handler provides HTTP request handlers.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/response"
)

// TestHealthHandlerResponse tests the health endpoint response structure.
func TestHealthHandlerResponse(t *testing.T) {
	handler := NewHealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

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
}

// TestWriteSuccessResponseFormat tests success response has correct JSON structure.
func TestWriteSuccessResponseFormat(t *testing.T) {
	w := httptest.NewRecorder()
	testData := map[string]interface{}{"id": "01HXABCDEFGHJKMNPQRSTVWXYZ", "name": "test"}

	WriteSuccessResponse(w, testData, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp response.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Data == nil {
		t.Error("expected data in response")
	}
}

// TestWriteErrorResponseFormat tests error response has correct structure.
func TestWriteErrorResponseFormat(t *testing.T) {
	w := httptest.NewRecorder()
	testErr := &domain.UserNotFoundError{Sub: "01HX"}

	WriteErrorResponse(w, testErr)

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

// TestGetUserHandler tests the GET user endpoint with a sub-keyed store.
func TestGetUserHandler(t *testing.T) {
	const existingSub = "01HXABCDEFGHJKMNPQRSTVWXYZ"
	const missingSub = "01HXAAAAAAAAAAAAAAAAAAAAAA"

	cases := []struct {
		name           string
		sub            string
		seed           bool
		expectedStatus int
	}{
		{name: "get existing user", sub: existingSub, seed: true, expectedStatus: http.StatusOK},
		{name: "get non-existent user", sub: missingSub, seed: false, expectedStatus: http.StatusNotFound},
		{name: "invalid sub", sub: "not-a-ulid", seed: false, expectedStatus: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := inmemory.NewUserRepository()
			if tc.seed {
				if _, err := repo.Upsert(context.Background(), &domain.User{Sub: tc.sub, DisplayNameCached: "Alice"}); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			getUC := userusecase.NewGetUserUseCase(repo)
			h := NewUserHandler(getUC, nil, nil, nil)

			req := httptest.NewRequest("GET", "/users/"+tc.sub, nil)
			req.SetPathValue("sub", tc.sub)
			w := httptest.NewRecorder()

			h.GetUser(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}
