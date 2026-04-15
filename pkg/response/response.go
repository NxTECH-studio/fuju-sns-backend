// Package response provides HTTP response types.
package response

import (
	"time"
)

// Meta contains metadata for responses
type Meta struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"request_id,omitempty"`
}

// SuccessResponse represents a successful API response
type SuccessResponse struct {
	Data interface{} `json:"data"`
	Meta *Meta       `json:"meta,omitempty"`
}

// ListResponse represents a paginated list response
type ListResponse struct {
	Data   interface{} `json:"data"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
	Total  int         `json:"total"`
	Meta   *Meta       `json:"meta,omitempty"`
}

// ErrorResponse represents an error API response
type ErrorResponse struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}
