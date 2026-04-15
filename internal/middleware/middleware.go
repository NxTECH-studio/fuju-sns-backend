// Package middleware provides HTTP middleware functions.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/logger"
	"github.com/fuju/backend/pkg/response"
)

const (
	// ContentTypeJSON is the content type header for JSON responses.
	ContentTypeJSON = "application/json"
)

// ResponseWriter wraps http.ResponseWriter to capture status code
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

// LoggingMiddleware logs HTTP requests and responses
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

// extractUserIDFromAuthHeader validates JWT token and extracts user ID
func extractUserIDFromAuthHeader(tokenManager *auth.TokenManager, authHeader string) (int64, bool) {
	if authHeader == "" {
		return 0, false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return 0, false
	}

	claims, err := tokenManager.ValidateToken(parts[1])
	if err != nil {
		return 0, false
	}

	return claims.UserID, true
}

// AuthMiddleware validates JWT tokens or session cookies
func AuthMiddleware(tokenManager *auth.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			userID, found := extractUserIDFromAuthHeader(tokenManager, authHeader)

			if !found {
				errResp := response.ErrorResponse{
					Code:      errors.ErrUnauthorized,
					Message:   "Missing or invalid authentication",
					Timestamp: time.Now(),
				}
				writeJSONErrorResponse(w, http.StatusUnauthorized, errResp)
				return
			}

			ctx := auth.SetUserInContext(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RecoveryMiddleware handles panics
func RecoveryMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					panicErr := fmt.Errorf("panic recovered: %v", err)
					log.Error(r.Context(), "Panic recovered", panicErr)
					errResp := response.ErrorResponse{
						Code:      errors.ErrInternal,
						Message:   "Internal server error",
						Timestamp: time.Now(),
					}
					writeJSONErrorResponse(w, http.StatusInternalServerError, errResp)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds CORS headers
func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeJSONErrorResponse writes a JSON error response with appropriate status code
func writeJSONErrorResponse(w http.ResponseWriter, statusCode int, errResp response.ErrorResponse) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		// Log encoding error but don't stop processing
		_ = err
	}
}

// ContextTimeoutMiddleware adds a timeout to context
func ContextTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
