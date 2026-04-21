// Package domain contains domain models and errors.
package domain

import (
	"context"
	"time"
)

// Image is a stored image record. ID and UserID are ULIDs generated in the
// application layer.
type Image struct {
	ID         string
	StorageKey string
	FileName   string
	MimeType   string
	FileSize   int64
	PublicURL  string
	UserID     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
}

// UploadImageRequest represents an image upload request.
type UploadImageRequest struct {
	FileName string
	FileData []byte
	MimeType string
	FileSize int64
	UserID   string
}

// StorageService defines file storage operations.
type StorageService interface {
	Upload(ctx context.Context, req *UploadImageRequest) (storageKey, publicURL string, err error)
	Delete(ctx context.Context, storageKey string) error
	GetPublicURL(storageKey string) string
}
