// Package domain contains domain models and errors.
package domain

import (
	"context"
	"time"
)

// Image represents an uploaded image entity
type Image struct {
	ID         string    // UUID
	StorageKey string    // Path in R2 storage (e.g., images/uuid/filename)
	FileName   string    // Original file name
	MimeType   string    // Content type (e.g., image/jpeg)
	FileSize   int64     // Size in bytes
	PublicURL  string    // Public accessible URL
	CreatedAt  time.Time // Creation timestamp
	UpdatedAt  time.Time // Last update timestamp
	DeletedAt  *time.Time // Soft delete timestamp
	UserID     int64     // User who uploaded the image
}

// UploadImageRequest represents an image upload request
type UploadImageRequest struct {
	FileName string // Original file name
	FileData []byte // File content
	MimeType string // Content type
	FileSize int64  // File size in bytes
	UserID   int64  // User ID
}

// StorageService defines file storage operations
type StorageService interface {
	// Upload uploads a file to R2 storage
	Upload(ctx context.Context, req *UploadImageRequest) (storageKey, publicURL string, err error)

	// Delete deletes a file from R2 storage
	Delete(ctx context.Context, storageKey string) error

	// GetPublicURL returns the public URL for a storage key
	GetPublicURL(storageKey string) string
}
