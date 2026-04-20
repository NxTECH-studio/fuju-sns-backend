// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/fuju/backend/internal/domain"
	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/response"
)

const (
	// ContentTypeJSON is the content type header for JSON responses.
	ContentTypeJSON = "application/json"
)

// ulidPattern validates a ULID (26 chars of Crockford Base32).
var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// UserHandler contains handlers for user endpoints.
type UserHandler struct {
	getUser     *userusecase.GetUserUseCase
	updateUser  *userusecase.UpdateUserProfileUseCase
	listUsers   *userusecase.ListUsersUseCase
	hydrateUser *userusecase.GetOrHydrateUserUseCase
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(
	getUser *userusecase.GetUserUseCase,
	updateUser *userusecase.UpdateUserProfileUseCase,
	listUsers *userusecase.ListUsersUseCase,
	hydrateUser *userusecase.GetOrHydrateUserUseCase,
) *UserHandler {
	return &UserHandler{
		getUser:     getUser,
		updateUser:  updateUser,
		listUsers:   listUsers,
		hydrateUser: hydrateUser,
	}
}

// publicUserView strips admin-only fields from a user record for public
// endpoints (e.g., looking up someone else's profile).
type publicUserView struct {
	Sub                string     `json:"sub"`
	DisplayNameCached  string     `json:"display_name"`
	DisplayIDCached    string     `json:"display_id"`
	IconURLCached      string     `json:"icon_url"`
	Bio                string     `json:"bio"`
	BannerURL          string     `json:"banner_url"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
	ProfileRefreshedAt time.Time  `json:"profile_refreshed_at"`
}

// selfUserView is the view returned for the caller's own identity (/me,
// profile update responses). It includes the is_admin flag.
type selfUserView struct {
	publicUserView
	IsAdmin bool `json:"is_admin"`
}

func toPublicView(u *domain.User) publicUserView {
	return publicUserView{
		Sub:                u.Sub,
		DisplayNameCached:  u.DisplayNameCached,
		DisplayIDCached:    u.DisplayIDCached,
		IconURLCached:      u.IconURLCached,
		Bio:                u.Bio,
		BannerURL:          u.BannerURL,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
		DeletedAt:          u.DeletedAt,
		ProfileRefreshedAt: u.ProfileRefreshedAt,
	}
}

func toSelfView(u *domain.User) selfUserView {
	return selfUserView{publicUserView: toPublicView(u), IsAdmin: u.IsAdmin}
}

// GetUser handles GET /users/{sub}.
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	sub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}

	user, err := h.getUser.Execute(r.Context(), sub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, toPublicView(user), http.StatusOK)
}

// Me handles GET /me — returns the authenticated user's full (self) view.
// Requires AuthMiddleware to have populated the sub on the context.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	if user, ok := auth.GetCurrentUserFromContext(r.Context()); ok {
		WriteSuccessResponse(w, toSelfView(user), http.StatusOK)
		return
	}

	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	user, err := h.hydrateUser.Execute(r.Context(), sub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, toSelfView(user), http.StatusOK)
}

// UpdateUser handles PUT /users/{sub} — only the authenticated user can
// modify their own profile, and only bio / banner_url are writable.
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	sub, ok := parseSubFromPath(w, r, "sub")
	if !ok {
		return
	}

	currentSub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	req, ok := parseUpdateProfileRequest(w, r)
	if !ok {
		return
	}

	user, err := h.updateUser.Execute(r.Context(), sub, currentSub, req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, toSelfView(user), http.StatusOK)
}

// parseSubFromPath extracts a sub (ULID) from a path variable with format
// validation.
func parseSubFromPath(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	sub := r.PathValue(name)
	if !ulidPattern.MatchString(sub) {
		WriteErrorResponse(w, errors.InvalidRequest("invalid sub (expected ULID)", nil))
		return "", false
	}
	return sub, true
}

func parseUpdateProfileRequest(w http.ResponseWriter, r *http.Request) (*domain.UpdateUserProfileRequest, bool) {
	var req domain.UpdateUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("invalid request body", err))
		return nil, false
	}
	return &req, true
}

// ListUsers handles GET /users.
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationParams(r)

	users, total, err := h.listUsers.Execute(r.Context(), limit, offset)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	views := make([]publicUserView, len(users))
	for i, u := range users {
		views[i] = toPublicView(u)
	}

	resp := response.ListResponse{
		Data:   views,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	WriteListResponse(w, &resp, http.StatusOK)
}

// parsePaginationParams extracts limit and offset from query parameters.
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

// writeJSONResponse writes a JSON response with proper headers.
func writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		_ = err
	}
}

// WriteSuccessResponse writes a successful JSON response.
func WriteSuccessResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	writeJSONResponse(w, response.SuccessResponse{Data: data}, statusCode)
}

// WriteListResponse writes a list response.
func WriteListResponse(w http.ResponseWriter, data *response.ListResponse, statusCode int) {
	writeJSONResponse(w, data, statusCode)
}

// WriteErrorResponse writes an error response.
func WriteErrorResponse(w http.ResponseWriter, err error) {
	statusCode := errors.ToHTTPStatus(err)
	errResp := buildErrorResponse(err)
	writeJSONResponse(w, errResp, statusCode)
}

// buildErrorResponse constructs an error response from an error.
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

// HealthHandler contains handlers for health endpoints.
type HealthHandler struct{}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health handles GET /health.
func (h *HealthHandler) Health(w http.ResponseWriter, _ *http.Request) {
	resp := response.HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	writeJSONResponse(w, resp, http.StatusOK)
}
