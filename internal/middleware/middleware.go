// Package middleware provides HTTP middleware functions.
package middleware

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	userusecase "github.com/fuju/backend/internal/usecase/user"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/authcore"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/response"
)

const (
	// ContentTypeJSON is the content type header for JSON responses.
	ContentTypeJSON = "application/json"
)

// ResponseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if !rw.written {
		rw.statusCode = statusCode
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// LoggingMiddleware logs HTTP requests and responses.
func LoggingMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Milliseconds()
			log.Info(r.Context(), "HTTP Request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.statusCode,
				"duration_ms", duration,
			)
		})
	}
}

// AuthMiddlewareConfig configures AuthMiddleware's token sources. The
// zero value is safe when only the Authorization header is used; a
// non-empty CookieName opts the middleware into Cookie fallback for
// browsers that use the POST /v1/auth/session handoff.
type AuthMiddlewareConfig struct {
	// CookieName is the HttpOnly cookie that carries the access token
	// (e.g. "fuju_access"). Empty disables the cookie path — useful
	// for deployments that only accept Bearer.
	CookieName string
}

// AuthMiddleware extracts the AuthCore access token from the request,
// introspects it via AuthCore, and stores the resolved sub, the raw
// access token, and the token expiry on the context. The token is
// sourced in priority order:
//
//  1. Authorization: Bearer <token> header
//  2. Cookie named cfg.CookieName (set by POST /v1/auth/session)
//
// Explicit beats implicit: if both are present the header wins so
// callers who know what they want (CLIs / mobile / backend-to-backend)
// can always override a stale Cookie. Fail-closed: missing / invalid
// token is 401, AuthCore outage is 503.
func AuthMiddleware(client authcore.Client, cfg AuthMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := extractAuthToken(r, cfg.CookieName)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, errors.ErrUnauthorized, "authentication required")
				return
			}

			session, err := client.Introspect(r.Context(), token)
			if err != nil {
				if stderrors.Is(err, authcore.ErrInvalidSession) {
					writeAuthError(w, http.StatusUnauthorized, errors.ErrUnauthorized, "invalid session")
					return
				}
				writeAuthError(w, http.StatusServiceUnavailable, errors.ErrExternalService, "authentication backend unavailable")
				return
			}

			ctx := auth.SetSubInContext(r.Context(), session.Sub)
			ctx = auth.SetAccessTokenInContext(ctx, token)
			ctx = auth.SetExpiresAtInContext(ctx, session.ExpiresAt)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractAuthToken returns the access token to introspect plus a bool
// indicating whether any source yielded a non-empty token. Header
// wins over cookie when both are present.
func extractAuthToken(r *http.Request, cookieName string) (string, bool) {
	if token, ok := extractBearerToken(r.Header.Get("Authorization")); ok {
		return token, true
	}
	if cookieName == "" {
		return "", false
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// extractBearerToken parses an `Authorization: Bearer <token>` header.
func extractBearerToken(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// AdminMiddleware requires the request's current user to have is_admin=true.
// It must run after AuthMiddleware + HydrateUserMiddleware so the user has
// been loaded onto the context.
func AdminMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.GetCurrentUserFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, errors.ErrUnauthorized, "authentication required")
				return
			}
			if !user.IsAdmin {
				writeAuthError(w, http.StatusForbidden, errors.ErrForbidden, "admin privilege required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HydrateUserMiddleware runs after AuthMiddleware and materialises the
// hydrated User onto the context.
func HydrateUserMiddleware(uc *userusecase.GetOrHydrateUserUseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sub, ok := auth.GetSubFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, errors.ErrUnauthorized, "authentication required")
				return
			}

			user, err := uc.Execute(r.Context(), sub)
			if err != nil {
				writeHydrateError(w, err)
				return
			}

			ctx := auth.SetCurrentUserInContext(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeHydrateError preserves the AppError code/status instead of
// steamrolling them into INTERNAL_ERROR.
func writeHydrateError(w http.ResponseWriter, err error) {
	if appErr, ok := errors.IsAppError(err); ok {
		writeAuthError(w, appErr.StatusCode, appErr.Code, appErr.Message)
		return
	}
	writeAuthError(w, http.StatusInternalServerError, errors.ErrInternal, "failed to load user")
}

// RecoveryMiddleware handles panics.
func RecoveryMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					panicErr := fmt.Errorf("panic recovered: %v", err)
					log.Error(r.Context(), "Panic recovered", panicErr)
					writeAuthError(w, http.StatusInternalServerError, errors.ErrInternal, "Internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds CORS headers based on allowed origins.
//
// Note on the Cookie handoff flow: `Access-Control-Allow-Credentials:
// true` is only emitted on the explicit-origin path. Browsers reject
// the combination `Allow-Origin: *` + `Allow-Credentials: true`, so
// deployments that issue the `fuju_access` cookie MUST set
// CORS_ALLOWED_ORIGINS to a comma-separated whitelist (never `*`).
// The `*` path remains for public, read-only, credential-less access.
func CORSMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				// Deliberately omit Allow-Credentials here: browsers
				// reject `*` + credentials, and Cookie-based auth
				// requires an explicit-origin configuration anyway.
			} else if isOriginAllowed(origin, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "3600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if the given origin is in the allowed origins list.
func isOriginAllowed(origin, allowedOrigins string) bool {
	if origin == "" {
		return false
	}

	origins := strings.Split(allowedOrigins, ",")
	for _, allowed := range origins {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}

// writeAuthError writes a JSON error response with an appropriate status.
func writeAuthError(w http.ResponseWriter, statusCode int, code, message string) {
	errResp := response.ErrorResponse{
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
	}
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errResp)
}

// ContextTimeoutMiddleware adds a timeout to context.
func ContextTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
