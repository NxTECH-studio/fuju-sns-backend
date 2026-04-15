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
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid user ID", err))
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
	_, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	var req domain.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return
	}

	user, err := h.createUser.Execute(r.Context(), &domain.CreateUserRequest{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Bio:         req.Bio,
		AvatarURL:   req.AvatarURL,
	})
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, user, http.StatusCreated)
}

// UpdateUser handles PUT /users/{id}
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid user ID", err))
		return
	}

	currentUserID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	var req domain.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return
	}

	user, err := h.updateUser.Execute(r.Context(), userID, currentUserID, &req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, user, http.StatusOK)
}

// ListUsers handles GET /users
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
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

// Helper functions

// WriteSuccessResponse writes a successful JSON response
func WriteSuccessResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response.SuccessResponse{Data: data})
}

// WriteListResponse writes a list response
func WriteListResponse(w http.ResponseWriter, data *response.ListResponse, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// WriteErrorResponse writes an error response
func WriteErrorResponse(w http.ResponseWriter, err error) {
	statusCode := errors.ToHTTPStatus(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

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

	json.NewEncoder(w).Encode(errResp)
}

// HealthHandler contains handlers for health endpoints
type HealthHandler struct{}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health handles GET /health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := response.HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(resp)
}
