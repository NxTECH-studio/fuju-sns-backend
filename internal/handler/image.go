// Package handler provides HTTP request handlers.
package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/fuju/backend/internal/domain"
	imageusecase "github.com/fuju/backend/internal/usecase/image"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/errors"
	"github.com/fuju/backend/pkg/response"
)

// Image upload limits.
const (
	// maxImageBytes is the hard per-file cap enforced before the usecase
	// even sees the data.
	maxImageBytes = 5 * 1024 * 1024
	// maxImageRequestBytes caps the whole multipart body, leaving a small
	// headroom for the envelope + headers.
	maxImageRequestBytes = maxImageBytes + 1024*1024
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

	// Hard-cap the request body before ParseMultipartForm so an attacker
	// cannot exhaust memory or force large disk spill.
	r.Body = http.MaxBytesReader(w, r.Body, maxImageRequestBytes)
	if err := r.ParseMultipartForm(maxImageRequestBytes); err != nil {
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

	if len(fileData) > maxImageBytes {
		WriteErrorResponse(w, errors.InvalidRequest("file size exceeds 5MB limit", nil))
		return
	}

	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = http.DetectContentType(fileData)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		WriteErrorResponse(w, errors.InvalidRequest("only image files are allowed", nil))
		return
	}

	// Cross-check advertised type against sniffed type so a client cannot
	// label HTML/JS as image/jpeg.
	sniffed := http.DetectContentType(fileData)
	if !strings.HasPrefix(sniffed, "image/") {
		WriteErrorResponse(w, errors.InvalidRequest("file content is not a recognised image", nil))
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

	imageID, ok := parseULIDFromPath(w, r, "id", "invalid image ID")
	if !ok {
		return
	}

	if err := h.deleteImage.Execute(r.Context(), imageID, sub); err != nil {
		WriteErrorResponse(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
