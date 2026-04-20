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

// ImageHandler contains handlers for image endpoints.
type ImageHandler struct {
	uploadImage   *imageusecase.UploadImageUseCase
	getUserImages *imageusecase.GetUserImagesUseCase
	deleteImage   *imageusecase.DeleteImageUseCase
}

// NewImageHandler creates a new ImageHandler.
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

// UploadImage handles POST /v1/images.
func (h *ImageHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	if err := r.ParseMultipartForm(6 * 1024 * 1024); err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("failed to parse form data", err))
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("file field is required", err))
		return
	}
	defer func() {
		_ = file.Close()
	}()

	fileData, err := io.ReadAll(file)
	if err != nil {
		WriteErrorResponse(w, errors.InvalidRequest("failed to read file data", err))
		return
	}

	if len(fileData) > 5*1024*1024 {
		WriteErrorResponse(w, errors.InvalidRequest("file size exceeds 5MB limit", nil))
		return
	}

	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	if len(mimeType) < 6 || mimeType[:6] != "image/" {
		WriteErrorResponse(w, errors.InvalidRequest("only image files are allowed", nil))
		return
	}

	req := &domain.UploadImageRequest{
		FileName: fileHeader.Filename,
		FileData: fileData,
		MimeType: mimeType,
		UserID:   sub,
	}

	uploadedImage, err := h.uploadImage.Execute(r.Context(), req)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	WriteSuccessResponse(w, uploadedImage, http.StatusCreated)
}

// GetUserImages handles GET /v1/images.
func (h *ImageHandler) GetUserImages(w http.ResponseWriter, r *http.Request) {
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	images, err := h.getUserImages.Execute(r.Context(), sub)
	if err != nil {
		WriteErrorResponse(w, err)
		return
	}

	listResp := &response.ListResponse{
		Data:   images,
		Limit:  100,
		Offset: 0,
		Total:  len(images),
	}

	WriteListResponse(w, listResp, http.StatusOK)
}

// DeleteImage handles DELETE /v1/images/{id}.
func (h *ImageHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	sub, ok := auth.GetSubFromContext(r.Context())
	if !ok {
		WriteErrorResponse(w, errors.Unauthorized("authentication required"))
		return
	}

	imageID := r.PathValue("id")
	if !ulidPattern.MatchString(imageID) {
		WriteErrorResponse(w, errors.InvalidRequest("invalid image ID", nil))
		return
	}

	if err := h.deleteImage.Execute(r.Context(), imageID, sub); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
