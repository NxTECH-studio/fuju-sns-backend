// Package response provides HTTP response types.
package response

import (
	"time"
)

// SuccessResponse represents a successful API response
type SuccessResponse struct {
	Data interface{} `json:"data"`
}

// ListResponse represents a paginated list response
type ListResponse struct {
	Data   interface{} `json:"data"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
	Total  int         `json:"total"`
}

// ErrorResponse represents an error API response
type ErrorResponse struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
