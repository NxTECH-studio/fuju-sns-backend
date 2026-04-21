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

// AuthMiddleware extracts the Bearer access token from the Authorization
// header, introspects it via AuthCore, and stores both the resolved sub and
// the raw access token on the context (the token is needed later to call
// AuthCore's profile endpoint). Fail-closed: an invalid token is 401; an
// upstream outage is 503.
func AuthMiddleware(client authcore.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := extractBearerToken(r.Header.Get("Authorization"))
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
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
func CORSMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
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
