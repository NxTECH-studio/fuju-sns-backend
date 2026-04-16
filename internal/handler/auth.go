// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/logger"
)

const (
	// Error messages
	errMethodNotAllowed       = "method not allowed"
	errInvalidRequestBody     = "invalid request body"
	errProviderRequired       = "provider is required"
	errCodeStateRequired      = "code and state are required"
	errRefreshTokenRequired   = "refresh token is required"
	errInvalidToken           = "invalid token"
	errFailedToEncodeResponse = "Failed to encode response"

	// HTTP headers and values
	headerContentType = "Content-Type"

	// success status for logout
	statusSuccess = "success"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	tokenManager *auth.TokenManager
	log          *logger.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(tokenManager *auth.TokenManager, log *logger.Logger) *AuthHandler {
	return &AuthHandler{
		tokenManager: tokenManager,
		log:          log,
	}
}

// OAuthAuthorizeRequest represents OAuth authorization request
type OAuthAuthorizeRequest struct {
	Provider    string `json:"provider"`
	RedirectURI string `json:"redirect_uri"`
}

// OAuthAuthorizeResponse represents OAuth authorization response
type OAuthAuthorizeResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// OAuthCallbackRequest represents OAuth callback request
type OAuthCallbackRequest struct {
	Code       string `json:"code"`
	State      string `json:"state"`
	DeviceType string `json:"device_type"`
}

// OAuthAuthorize generates OAuth authorization URL
func (h *AuthHandler) OAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		WriteErrorResponse(w, errors.New(errMethodNotAllowed))
		return
	}

	var req OAuthAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Warn(ctx, errInvalidRequestBody, "error", err.Error())
		WriteErrorResponse(w, errors.New(errInvalidRequestBody))
		return
	}

	// Validate provider
	if req.Provider == "" {
		WriteErrorResponse(w, errors.New(errProviderRequired))
		return
	}

	// Generate OAuth authorization URL based on provider
	// In production, this would construct proper OAuth URLs with client ID, scopes, etc.
	redirectURL := "https://accounts.google.com/o/oauth2/v2/auth?client_id=YOUR_CLIENT_ID&redirect_uri=" + req.RedirectURI
	if req.Provider == "github" {
		redirectURL = "https://github.com/login/oauth/authorize?client_id=YOUR_CLIENT_ID&redirect_uri=" + req.RedirectURI
	}

	resp := OAuthAuthorizeResponse{
		RedirectURL: redirectURL,
	}

	w.Header().Set(headerContentType, ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error(ctx, errFailedToEncodeResponse, err)
	}

	h.log.Info(ctx, "OAuth authorization initiated", "provider", req.Provider)
}

// OAuthCallback handles OAuth provider callback
func (h *AuthHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		WriteErrorResponse(w, errors.New(errMethodNotAllowed))
		return
	}

	var req OAuthCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Warn(ctx, errInvalidRequestBody, "error", err.Error())
		WriteErrorResponse(w, errors.New(errInvalidRequestBody))
		return
	}

	// Validate required fields
	if req.Code == "" || req.State == "" {
		WriteErrorResponse(w, errors.New(errCodeStateRequired))
		return
	}

	if req.DeviceType == "" {
		req.DeviceType = "web"
	}

	// Validate state token and exchange code for access token
	// In production: validate state for CSRF protection, exchange code with OAuth provider,
	// fetch user info, create/update user in database, generate session or JWT token
	authResp := map[string]interface{}{
		"session_id": "placeholder_session_id",
		"user": map[string]interface{}{
			"id":         1,
			"username":   "testuser",
			"email":      "test@example.com",
			"created_at": time.Now(),
		},
	}

	w.Header().Set(headerContentType, ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(authResp); err != nil {
		h.log.Error(ctx, errFailedToEncodeResponse, err)
	}

	h.log.Info(ctx, "OAuth callback processed", "device_type", req.DeviceType)
}

// RefreshTokenRequest represents token refresh request
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenResponse represents token refresh response
type RefreshTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// RefreshToken refreshes an access token
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		WriteErrorResponse(w, errors.New(errMethodNotAllowed))
		return
	}

	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.log.Warn(ctx, errInvalidRequestBody, "error", err.Error())
		WriteErrorResponse(w, errors.New(errInvalidRequestBody))
		return
	}

	// Validate refresh token
	if req.RefreshToken == "" {
		WriteErrorResponse(w, errors.New(errRefreshTokenRequired))
		return
	}

	// Validate refresh token signature and expiration time
	// In production: validate token, check blacklist, generate new access token
	// For now: verify token is not empty (real validation would use TokenManager)

	resp := RefreshTokenResponse{
		AccessToken: "new_access_token",
		ExpiresIn:   1800,
	}

	w.Header().Set(headerContentType, ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error(ctx, errFailedToEncodeResponse, err)
	}

	h.log.Info(ctx, "Token refreshed")
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		WriteErrorResponse(w, errors.New(errMethodNotAllowed))
		return
	}

	// Invalidate session/token in production:
	// - Remove from token blacklist for JWT
	// - Mark session as invalid in cache
	// - Clear any related session data

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	resp := map[string]interface{}{
		"status": statusSuccess,
	}

	w.Header().Set(headerContentType, ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error(ctx, errFailedToEncodeResponse, err)
	}

	h.log.Info(ctx, "User logged out")
}
