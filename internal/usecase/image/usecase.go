// Package image contains image business logic use cases.
package image

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
	"github.com/google/uuid"
)

// UploadImageUseCase represents the use case for uploading an image
type UploadImageUseCase struct {
	imageRepo      repository.ImageRepository
	storageService domain.StorageService
}

// NewUploadImageUseCase creates a new UploadImageUseCase
func NewUploadImageUseCase(
	imageRepo repository.ImageRepository,
	storageService domain.StorageService,
) *UploadImageUseCase {
	return &UploadImageUseCase{
		imageRepo:      imageRepo,
		storageService: storageService,
	}
}

// Execute uploads an image to R2 and saves metadata
func (uc *UploadImageUseCase) Execute(ctx context.Context, req *domain.UploadImageRequest) (*domain.Image, error) {
	if req == nil {
		return nil, errors.InvalidRequest("upload request is required", nil)
	}

	if len(req.FileData) == 0 {
		return nil, errors.InvalidRequest("file data is empty", nil)
	}

	if req.FileSize = int64(len(req.FileData)); req.FileSize > 5*1024*1024 { // 5MB limit
		return nil, errors.InvalidRequest("file size exceeds 5MB limit", nil)
	}

	// Upload to R2
	storageKey, publicURL, err := uc.storageService.Upload(ctx, req)
	if err != nil {
		return nil, err
	}

	// Generate UUID for image ID
	imageID := uuid.New().String()

	// Create image record
	image := &domain.Image{
		ID:         imageID,
		StorageKey: storageKey,
		FileName:   req.FileName,
		MimeType:   req.MimeType,
		FileSize:   req.FileSize,
		PublicURL:  publicURL,
		UserID:     req.UserID,
	}

	// Save to database
	createdImage, err := uc.imageRepo.Create(ctx, image)
	if err != nil {
		return nil, errors.New(
			errors.ErrDatabaseError,
			"failed to save image metadata",
			500,
			err,
		)
	}

	return createdImage, nil
}

// GetUserImagesUseCase represents the use case for retrieving user images
type GetUserImagesUseCase struct {
	imageRepo repository.ImageRepository
}

// NewGetUserImagesUseCase creates a new GetUserImagesUseCase
func NewGetUserImagesUseCase(imageRepo repository.ImageRepository) *GetUserImagesUseCase {
	return &GetUserImagesUseCase{imageRepo: imageRepo}
}

// Execute retrieves all images for a user
func (uc *GetUserImagesUseCase) Execute(ctx context.Context, userID int64) ([]*domain.Image, error) {
	if userID <= 0 {
		return nil, errors.InvalidRequest("invalid user ID", nil)
	}

	images, err := uc.imageRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, errors.New(
			errors.ErrDatabaseError,
			"failed to retrieve user images",
			500,
			err,
		)
	}

	return images, nil
}

// DeleteImageUseCase represents the use case for deleting an image
type DeleteImageUseCase struct {
	imageRepo      repository.ImageRepository
	storageService domain.StorageService
}

// NewDeleteImageUseCase creates a new DeleteImageUseCase
func NewDeleteImageUseCase(
	imageRepo repository.ImageRepository,
	storageService domain.StorageService,
) *DeleteImageUseCase {
	return &DeleteImageUseCase{
		imageRepo:      imageRepo,
		storageService: storageService,
	}
}

// Execute deletes an image from R2 and database
func (uc *DeleteImageUseCase) Execute(ctx context.Context, imageID string, userID int64) error {
	if imageID == "" {
		return errors.InvalidRequest("image ID is required", nil)
	}

	// Retrieve image
	image, err := uc.imageRepo.GetByID(ctx, imageID)
	if err != nil {
		return errors.New(
			errors.ErrDatabaseError,
			"failed to retrieve image",
			500,
			err,
		)
	}

	if image == nil {
		return errors.NotFound("image not found")
	}

	// Verify ownership
	if image.UserID != userID {
		return errors.Forbidden("not authorized to delete this image")
	}

	// Delete from R2
	if err := uc.storageService.Delete(ctx, image.StorageKey); err != nil {
		// Log error but continue with database deletion
		_ = err
	}

	// Soft-delete from database
	if err := uc.imageRepo.Delete(ctx, imageID); err != nil {
		return errors.New(
			errors.ErrDatabaseError,
			"failed to delete image",
			500,
			err,
		)
	}

	return nil
}
