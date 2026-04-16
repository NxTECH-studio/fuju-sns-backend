// Package handler provides HTTP request handlers.
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

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
	oauthClientID    string
	oauthRedirectURL string
	frontendURL      string
	tokenManager     *auth.TokenManager
	log              *logger.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(oauthClientID, oauthRedirectURL, frontendURL string, tokenManager *auth.TokenManager, log *logger.Logger) *AuthHandler {
	return &AuthHandler{
		oauthClientID:    oauthClientID,
		oauthRedirectURL: oauthRedirectURL,
		frontendURL:      frontendURL,
		tokenManager:     tokenManager,
		log:              log,
	}
}

// OAuthAuthorizeRequest represents OAuth authorization request
type OAuthAuthorizeRequest struct {
	Provider string `json:"provider"`
	// Note: redirect_uri is configured server-side via OAUTH_REDIRECT_URL
	// This field is deprecated but kept for backward compatibility
	RedirectURI string `json:"redirect_uri,omitempty"`
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

	// Generate CSRF protection state token
	stateToken := make([]byte, 16)
	if _, err := rand.Read(stateToken); err != nil {
		h.log.Error(ctx, "failed to generate state token", err)
		WriteErrorResponse(w, errors.New("failed to generate state token"))
		return
	}
	state := hex.EncodeToString(stateToken)

	// Generate OAuth authorization URL based on provider
	// Use server-configured redirect_uri for security
	var redirectURL string
	switch req.Provider {
	case "google":
		params := url.Values{
			"client_id":     {h.oauthClientID},
			"redirect_uri":  {h.oauthRedirectURL},
			"response_type": {"code"},
			"scope":         {"openid email profile"},
			"state":         {state},
		}
		redirectURL = "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()

	case "github":
		params := url.Values{
			"client_id":    {h.oauthClientID},
			"redirect_uri": {h.oauthRedirectURL},
			"scope":        {"user:email"},
			"state":        {state},
		}
		redirectURL = "https://github.com/login/oauth/authorize?" + params.Encode()

	default:
		WriteErrorResponse(w, errors.New("unsupported provider"))
		return
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
// Supports both GET (from OAuth provider redirect) and POST (from frontend)
func (h *AuthHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var code, state string

	// OAuth providers redirect with GET request and query parameters
	// Frontend can also POST with JSON body for flexibility
	if r.Method == http.MethodGet {
		// Parse query parameters from OAuth provider redirect
		query := r.URL.Query()
		code = query.Get("code")
		state = query.Get("state")

		// Check for OAuth provider error
		errMsg := query.Get("error")
		errDesc := query.Get("error_description")
		if errMsg != "" {
			h.log.Warn(ctx, "OAuth provider returned error", "error", errMsg, "description", errDesc)
			// Redirect to frontend with error
			errorURL := h.frontendURL + "/auth/callback?error=" + url.QueryEscape(errMsg)
			if errDesc != "" {
				errorURL += "&error_description=" + url.QueryEscape(errDesc)
			}
			http.Redirect(w, r, errorURL, http.StatusFound)
			return
		}

	} else if r.Method == http.MethodPost {
		// Handle legacy POST requests with JSON body
		var req OAuthCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.log.Warn(ctx, errInvalidRequestBody, "error", err.Error())
			WriteErrorResponse(w, errors.New(errInvalidRequestBody))
			return
		}
		code = req.Code
		state = req.State

	} else {
		WriteErrorResponse(w, errors.New(errMethodNotAllowed))
		return
	}

	// Validate required fields
	if code == "" || state == "" {
		if r.Method == http.MethodGet {
			// Redirect to frontend with error for GET
			http.Redirect(w, r, h.frontendURL+"/auth/callback?error=missing_code_or_state", http.StatusFound)
		} else {
			// JSON error for POST
			WriteErrorResponse(w, errors.New(errCodeStateRequired))
		}
		return
	}

	// In production:
	// 1. Validate state token for CSRF protection (compare with stored state)
	// 2. Exchange authorization code for access token with OAuth provider
	// 3. Fetch user info from OAuth provider
	// 4. Create/update user in database
	// 5. Generate JWT token
	// 6. Redirect to frontend with token

	// For GET requests: Redirect to frontend with code and state
	if r.Method == http.MethodGet {
		redirectURL := h.frontendURL + "/auth/callback?" +
			url.Values{
				"code":  {code},
				"state": {state},
			}.Encode()
		http.Redirect(w, r, redirectURL, http.StatusFound)
		h.log.Info(ctx, "OAuth callback redirected to frontend", "method", r.Method)
		return
	}

	// For POST requests: return success response
	authResp := map[string]interface{}{
		"success": true,
		"message": "OAuth callback received",
		"code":    code,
		"state":   state,
	}

	w.Header().Set(headerContentType, ContentTypeJSON)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(authResp); err != nil {
		h.log.Error(ctx, errFailedToEncodeResponse, err)
	}

	h.log.Info(ctx, "OAuth callback processed", "method", r.Method)
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
