package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError represents an application error
type AppError struct {
	Code       string // e.g., "INVALID_REQUEST", "USER_NOT_FOUND"
	Message    string
	StatusCode int
	Err        error // underlying error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a new AppError
func New(code, message string, statusCode int, err error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
		Err:        err,
	}
}

// NewWithStatus creates a new AppError with HTTP status code
func NewWithStatus(code, message string, statusCode int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// Common error codes and constructors
const (
	// Client errors
	ErrInvalidRequest   = "INVALID_REQUEST"
	ErrUnauthorized     = "UNAUTHORIZED"
	ErrForbidden        = "FORBIDDEN"
	ErrNotFound         = "NOT_FOUND"
	ErrConflict         = "CONFLICT"
	ErrValidationFailed = "VALIDATION_FAILED"

	// Server errors
	ErrInternal        = "INTERNAL_ERROR"
	ErrDatabaseError   = "DATABASE_ERROR"
	ErrExternalService = "EXTERNAL_SERVICE_ERROR"
)

// Error constructors
func InvalidRequest(message string, err error) *AppError {
	return New(ErrInvalidRequest, message, http.StatusBadRequest, err)
}

func Unauthorized(message string) *AppError {
	return NewWithStatus(ErrUnauthorized, message, http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	return NewWithStatus(ErrForbidden, message, http.StatusForbidden)
}

func NotFound(message string) *AppError {
	return NewWithStatus(ErrNotFound, message, http.StatusNotFound)
}

func Conflict(message string) *AppError {
	return NewWithStatus(ErrConflict, message, http.StatusConflict)
}

func ValidationFailed(message string) *AppError {
	return NewWithStatus(ErrValidationFailed, message, http.StatusBadRequest)
}

func InternalServer(message string, err error) *AppError {
	return New(ErrInternal, message, http.StatusInternalServerError, err)
}

func DatabaseError(message string, err error) *AppError {
	return New(ErrDatabaseError, message, http.StatusInternalServerError, err)
}

func ExternalServiceError(message string, err error) *AppError {
	return New(ErrExternalService, message, http.StatusBadGateway, err)
}

// ToHTTPStatus converts an error to an HTTP status code
func ToHTTPStatus(err error) int {
	if appErr, ok := IsAppError(err); ok {
		return appErr.StatusCode
	}
	return http.StatusInternalServerError
}

// ToErrorResponse converts an error to a standardized error response
type ErrorResponse struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Timestamp string      `json:"timestamp"`
	Details   interface{} `json:"details,omitempty"`
}

// NewErrorResponse creates a new error response from AppError
func NewErrorResponse(err error, details interface{}) *ErrorResponse {
	var code, message string

	if appErr, ok := IsAppError(err); ok {
		code = appErr.Code
		message = appErr.Message
	} else {
		code = ErrInternal
		message = "Internal Server Error"
	}

	return &ErrorResponse{
		Code:      code,
		Message:   message,
		Timestamp: "",
		Details:   details,
	}
}
