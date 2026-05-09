// Package image contains image business logic use cases.
package image

import (
	"context"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/logger"
	"github.com/oklog/ulid/v2"
)

// UploadImageUseCase represents the use case for uploading an image.
type UploadImageUseCase struct {
	imageRepo      repository.ImageRepository
	storageService domain.StorageService
}

// NewUploadImageUseCase creates a new UploadImageUseCase.
func NewUploadImageUseCase(
	imageRepo repository.ImageRepository,
	storageService domain.StorageService,
) *UploadImageUseCase {
	return &UploadImageUseCase{
		imageRepo:      imageRepo,
		storageService: storageService,
	}
}

// Execute uploads an image to R2 and saves metadata.
func (uc *UploadImageUseCase) Execute(ctx context.Context, req *domain.UploadImageRequest) (*domain.Image, error) {
	if req == nil {
		return nil, errors.InvalidRequest("upload request is required", nil)
	}

	if len(req.FileData) == 0 {
		return nil, errors.InvalidRequest("file data is empty", nil)
	}

	req.FileSize = int64(len(req.FileData))
	if req.FileSize > domain.MaxImageBytes {
		return nil, errors.InvalidRequest("file size exceeds 5MB limit", nil)
	}

	storageKey, publicURL, err := uc.storageService.Upload(ctx, req)
	if err != nil {
		return nil, err
	}

	image := &domain.Image{
		ID:         ulid.Make().String(),
		StorageKey: storageKey,
		FileName:   req.FileName,
		MimeType:   req.MimeType,
		FileSize:   req.FileSize,
		PublicURL:  publicURL,
		UserID:     req.UserID,
	}

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

// GetUserImagesUseCase represents the use case for retrieving user images.
type GetUserImagesUseCase struct {
	imageRepo repository.ImageRepository
}

// NewGetUserImagesUseCase creates a new GetUserImagesUseCase.
func NewGetUserImagesUseCase(imageRepo repository.ImageRepository) *GetUserImagesUseCase {
	return &GetUserImagesUseCase{imageRepo: imageRepo}
}

// Execute retrieves all images for a user.
func (uc *GetUserImagesUseCase) Execute(ctx context.Context, userSub string) ([]*domain.Image, error) {
	if userSub == "" {
		return nil, errors.InvalidRequest("invalid user sub", nil)
	}

	images, err := uc.imageRepo.GetByUserID(ctx, userSub)
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

// DeleteImageUseCase represents the use case for deleting an image.
type DeleteImageUseCase struct {
	imageRepo      repository.ImageRepository
	storageService domain.StorageService
	log            *logger.Logger
}

// NewDeleteImageUseCase creates a new DeleteImageUseCase. log may be nil
// in tests; in that case the storage-delete failure path is silent
// instead of WARN-logged.
func NewDeleteImageUseCase(
	imageRepo repository.ImageRepository,
	storageService domain.StorageService,
	log *logger.Logger,
) *DeleteImageUseCase {
	return &DeleteImageUseCase{
		imageRepo:      imageRepo,
		storageService: storageService,
		log:            log,
	}
}

// Execute deletes an image from R2 and database.
func (uc *DeleteImageUseCase) Execute(ctx context.Context, imageID, userSub string) error {
	if imageID == "" {
		return errors.InvalidRequest("image ID is required", nil)
	}

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

	if image.UserID != userSub {
		return errors.Forbidden("not authorized to delete this image")
	}

	// Best-effort remove from R2; continue on error so the DB row is still
	// soft-deleted. WARN so an operator can find orphaned objects later
	// — silencing this leaves R2 storage growing without a trail.
	if err := uc.storageService.Delete(ctx, image.StorageKey); err != nil {
		uc.warn(ctx, "image storage delete failed; DB will still be soft-deleted",
			"image_id", imageID,
			"storage_key", image.StorageKey,
			"err", err,
		)
	}

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

func (uc *DeleteImageUseCase) warn(ctx context.Context, msg string, kv ...any) {
	if uc.log == nil {
		return
	}
	uc.log.Warn(ctx, msg, kv...)
}
