// Package handler provides HTTP request handlers.
package handler

import (
	"io"
	"net/http"

	"github.com/fuju/backend/internal/domain"
	imageusecase "github.com/fuju/backend/internal/usecase/image"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/response"
)

// ImageHandler contains handlers for image endpoints
type ImageHandler struct {
	uploadImage   *imageusecase.UploadImageUseCase
	getUserImages *imageusecase.GetUserImagesUseCase
	deleteImage   *imageusecase.DeleteImageUseCase
}

// NewImageHandler creates a new ImageHandler
func NewImageHandler(
	uploadImage *imageusecase.UploadImageUseCase,
	getUserImages *imageusecase.GetUserImagesUseCase,
	deleteImage *imageusecase.DeleteImageUseCase,
) *ImageHandler {
	return &ImageHandler{
		uploadImage:   uploadImage,
		getUserImages: getUserImages,
		deleteImage:   deleteImage,
	}
}

// UploadImage handles POST /v1/images
func (h *ImageHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	// Verify authentication
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	// Parse multipart form (max 6MB)
	if err := r.ParseMultipartForm(6 * 1024 * 1024); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("failed to parse form data", err))
		return
	}

	// Get file from form
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("file field is required", err))
		return
	}
	defer file.Close()

	// Read file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("failed to read file data", err))
		return
	}

	// Validate file size (5MB limit)
	if len(fileData) > 5*1024*1024 {
		WriteErrorResponse(w, errors.InvalidRequest("file size exceeds 5MB limit", nil))
		return
	}

	// Get MIME type from Content-Type header
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Validate MIME type (only image/* types allowed)
	if len(mimeType) < 6 || mimeType[:6] != "image/" {
		WriteErrorResponse(w, errors.InvalidRequest("only image files are allowed", nil))
		return
	}

	// Create upload request
	req := &domain.UploadImageRequest{
		FileName: fileHeader.Filename,
		FileData: fileData,
		MimeType: mimeType,
		UserID:   userID,
	}

	// Execute upload use case
	uploadedImage, err := h.uploadImage.Execute(r.Context(), req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, uploadedImage, http.StatusCreated)
}

// GetUserImages handles GET /v1/images
func (h *ImageHandler) GetUserImages(w http.ResponseWriter, r *http.Request) {
	// Verify authentication
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	// Execute get user images use case
	images, err := h.getUserImages.Execute(r.Context(), userID)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	// Prepare list response
	listResp := &response.ListResponse{
		Data:   images,
		Limit:  100,
		Offset: 0,
		Total:  len(images),
	}

	WriteListResponse(w, listResp, http.StatusOK)
}

// DeleteImage handles DELETE /v1/images/{id}
func (h *ImageHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	// Verify authentication
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	// Get image ID from URL path
	imageID := r.PathValue("id")
	if imageID == "" {
		WriteErrorResponse(w, errors.InvalidRequest("image ID is required", nil))
		return
	}

	// Execute delete image use case
	if err := h.deleteImage.Execute(r.Context(), imageID, userID); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
