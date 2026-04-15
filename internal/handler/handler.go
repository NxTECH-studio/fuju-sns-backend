// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/fuju/backend/internal/domain"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/response"
)

const (
	ContentTypeJSON = "application/json"
)

// UserHandler contains handlers for user endpoints
type UserHandler struct {
	getUser    *userusecase.GetUserUseCase
	createUser *userusecase.CreateUserUseCase
	updateUser *userusecase.UpdateUserUseCase
	listUsers  *userusecase.ListUsersUseCase
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(
	getUser *userusecase.GetUserUseCase,
	createUser *userusecase.CreateUserUseCase,
	updateUser *userusecase.UpdateUserUseCase,
	listUsers *userusecase.ListUsersUseCase,
) *UserHandler {
	return &UserHandler{
		getUser:    getUser,
		createUser: createUser,
		updateUser: updateUser,
		listUsers:  listUsers,
	}
}

// GetUser handles GET /users/{id}
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDFromPath(w, r)
	if !ok {
		return
	}

	user, err := h.getUser.Execute(r.Context(), userID)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, user, http.StatusOK)
}

// CreateUser handles POST /users
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if !isAuthenticatedUser(w, r) {
		return
	}

	req, ok := parseCreateUserRequest(w, r)
	if !ok {
		return
	}

	user, err := h.createUser.Execute(r.Context(), req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, user, http.StatusCreated)
}

// isAuthenticatedUser checks if the request has valid authentication
func isAuthenticatedUser(w http.ResponseWriter, r *http.Request) bool {
	_, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return false
	}
	return true
}

// parseCreateUserRequest parses and validates the create user request
func parseCreateUserRequest(w http.ResponseWriter, r *http.Request) (*domain.CreateUserRequest, bool) {
	var req domain.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return nil, false
	}
	return &domain.CreateUserRequest{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Bio:         req.Bio,
		AvatarURL:   req.AvatarURL,
	}, true
}

// UpdateUser handles PUT /users/{id}
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDFromPath(w, r)
	if !ok {
		return
	}

	if !isAuthenticatedUser(w, r) {
		return
	}

	currentUserID, _ := auth.GetUserIDFromContext(r.Context())

	req, ok := parseUpdateUserRequest(w, r)
	if !ok {
		return
	}

	user, err := h.updateUser.Execute(r.Context(), userID, currentUserID, req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, user, http.StatusOK)
}

// parseUserIDFromPath extracts and parses the user ID from URL path
func parseUserIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid user ID", err))
		return 0, false
	}
	return userID, true
}

// parseUpdateUserRequest parses and validates the update user request
func parseUpdateUserRequest(w http.ResponseWriter, r *http.Request) (*domain.UpdateUserRequest, bool) {
	var req domain.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return nil, false
	}
	return &req, true
}

// ListUsers handles GET /users
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationParams(r)

	users, total, err := h.listUsers.Execute(r.Context(), limit, offset)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	resp := response.ListResponse{
		Data:   users,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	WriteListResponse(w, &resp, http.StatusOK)
}

// parsePaginationParams extracts limit and offset from query parameters
func parsePaginationParams(r *http.Request) (int, int) {
	limit := 20
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}

// Helper functions

// writeJSONResponse writes a JSON response with proper headers
func writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// WriteSuccessResponse writes a successful JSON response
func WriteSuccessResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	writeJSONResponse(w, response.SuccessResponse{Data: data}, statusCode)
}

// WriteListResponse writes a list response
func WriteListResponse(w http.ResponseWriter, data *response.ListResponse, statusCode int) {
	writeJSONResponse(w, data, statusCode)
}

// WriteErrorResponse writes an error response
func WriteErrorResponse(w http.ResponseWriter, err error) {
	statusCode := errors.ToHTTPStatus(err)
	errResp := buildErrorResponse(err)
	writeJSONResponse(w, errResp, statusCode)
}

// buildErrorResponse constructs an error response from an error
func buildErrorResponse(err error) response.ErrorResponse {
	errResp := response.ErrorResponse{
		Code:      errors.ErrInternal,
		Message:   "Internal server error",
		Timestamp: time.Now(),
	}

	if appErr, ok := errors.IsAppError(err); ok {
		errResp.Code = appErr.Code
		errResp.Message = appErr.Message
	} else {
		errResp.Message = err.Error()
	}

	return errResp
}

// HealthHandler contains handlers for health endpoints
type HealthHandler struct{}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health handles GET /health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	resp := response.HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}

	writeJSONResponse(w, resp, http.StatusOK)
}
