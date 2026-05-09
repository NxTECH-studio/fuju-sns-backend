package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	imageusecase "github.com/fuju/backend/internal/usecase/image"
	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/response"
)

// ULIDs use Crockford Base32: digits + A-Z minus I, L, O, U. The fixed
// strings below are hand-picked from that alphabet so parseULIDFromPath
// accepts them.
const (
	imageOwnerSub  = "01HMGAWNER000000000000000A"
	imageOtherSub  = "01HMGAWNER000000000000000B"
	missingImageID = "01HMGNAFTNDF00000000000000"
)

// fakeImageStorage is the handler-side double for domain.StorageService.
// The usecase tests already cover the storage interaction in detail —
// here we just need a backend that records keys and lets us inject
// failures.
type fakeImageStorage struct {
	objects   map[string][]byte
	deleted   []string
	uploadErr error
}

func newFakeImageStorage() *fakeImageStorage {
	return &fakeImageStorage{objects: make(map[string][]byte)}
}

func (s *fakeImageStorage) Upload(_ context.Context, req *domain.UploadImageRequest) (string, string, error) {
	if s.uploadErr != nil {
		return "", "", s.uploadErr
	}
	key := "images/" + req.UserID + "/test/" + req.FileName
	cp := make([]byte, len(req.FileData))
	copy(cp, req.FileData)
	s.objects[key] = cp
	return key, "https://cdn.example.com/" + key, nil
}

func (s *fakeImageStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

func (s *fakeImageStorage) GetPublicURL(key string) string {
	return "https://cdn.example.com/" + key
}

// imageFixture bundles the handler with the fake storage so individual
// tests can pre-seed images via the upload handler and inspect the
// storage side-effects (deleted keys, in-flight objects).
type imageFixture struct {
	handler *ImageHandler
	storage *fakeImageStorage
}

func newImageFixture(t *testing.T) *imageFixture {
	t.Helper()
	links := inmemory.NewLinkStore()
	repo := inmemory.NewImageRepository(links)
	storage := newFakeImageStorage()

	uploadUC := imageusecase.NewUploadImageUseCase(repo, storage)
	getUC := imageusecase.NewGetUserImagesUseCase(repo)
	deleteUC := imageusecase.NewDeleteImageUseCase(repo, storage)

	return &imageFixture{
		handler: NewImageHandler(uploadUC, getUC, deleteUC),
		storage: storage,
	}
}

// buildMultipartRequest assembles a POST /v1/images request with the
// given file payload. fieldName is the form field; the handler expects
// "file" but we expose it so tests can probe the missing-field case.
func buildMultipartRequest(t *testing.T, fieldName, filename, contentType string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="`+fieldName+`"; filename="`+filename+`"`)
	if contentType != "" {
		hdr.Set("Content-Type", contentType)
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// fakeJPEG returns a tiny byte slice whose first bytes match the JPEG
// magic number that http.DetectContentType uses for sniffing.
func fakeJPEG() []byte {
	// JPEG SOI + APP0 marker is enough to be sniffed as image/jpeg.
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
}

func authedReq(req *http.Request, sub string) *http.Request {
	return req.WithContext(auth.SetSubInContext(req.Context(), sub))
}

func TestUploadImage_Success(t *testing.T) {
	fx := newImageFixture(t)
	req := buildMultipartRequest(t, "file", "cat.jpg", "image/jpeg", fakeJPEG())
	req = authedReq(req, imageOwnerSub)

	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", w.Code, w.Body.String())
	}

	var resp struct {
		Data publicImageView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.ID == "" {
		t.Errorf("expected non-empty id")
	}
	if resp.Data.PublicURL == "" {
		t.Errorf("expected non-empty public_url")
	}
	if resp.Data.UserID != imageOwnerSub {
		t.Errorf("user_id = %q, want %q", resp.Data.UserID, imageOwnerSub)
	}
	if resp.Data.MimeType != "image/jpeg" {
		t.Errorf("mime_type = %q, want image/jpeg", resp.Data.MimeType)
	}

	// Belt-and-suspenders: storage_key must not be present on the wire.
	// (publicImageView omits the field, so unmarshal would silently drop
	// it — sniff the raw body for the substring.)
	if strings.Contains(w.Body.String(), "storage_key") {
		t.Errorf("response body must not include storage_key: %s", w.Body.String())
	}
	// Snake-case sanity: the JSON tag was easy to miss when the type was
	// added; pin the keys we promise to the FE.
	for _, key := range []string{`"id"`, `"file_name"`, `"mime_type"`, `"file_size"`, `"public_url"`, `"user_id"`, `"created_at"`, `"updated_at"`} {
		if !strings.Contains(w.Body.String(), key) {
			t.Errorf("expected response body to contain %s, got %s", key, w.Body.String())
		}
	}
}

func TestUploadImage_RequiresAuth(t *testing.T) {
	fx := newImageFixture(t)
	req := buildMultipartRequest(t, "file", "cat.jpg", "image/jpeg", fakeJPEG())
	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUploadImage_MissingFileField(t *testing.T) {
	fx := newImageFixture(t)
	// Use a non-"file" form field to trigger r.FormFile("file") error.
	req := buildMultipartRequest(t, "not_file", "cat.jpg", "image/jpeg", fakeJPEG())
	req = authedReq(req, imageOwnerSub)

	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestUploadImage_OverSizeLimit(t *testing.T) {
	fx := newImageFixture(t)
	// 5 MiB + 1 byte tips the in-handler cap. The MaxBytesReader on the
	// whole body has 1 MiB headroom, so this also exercises the per-file
	// `len(fileData) > maxImageBytes` branch (not the body cap).
	body := make([]byte, 5*1024*1024+1)
	// Make the bytes look like JPEG so sniff doesn't independently 400
	// before the size check fires.
	copy(body, fakeJPEG())
	req := buildMultipartRequest(t, "file", "big.jpg", "image/jpeg", body)
	req = authedReq(req, imageOwnerSub)

	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUploadImage_MIMEMismatchHTMLAsJPEG(t *testing.T) {
	fx := newImageFixture(t)
	htmlBody := []byte("<!doctype html><html><body>not an image</body></html>")
	// Advertised image/jpeg but the sniff result is text/html.
	req := buildMultipartRequest(t, "file", "evil.jpg", "image/jpeg", htmlBody)
	req = authedReq(req, imageOwnerSub)

	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for sniff mismatch, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestUploadImage_RejectsNonImageContentType(t *testing.T) {
	fx := newImageFixture(t)
	req := buildMultipartRequest(t, "file", "notes.txt", "text/plain", []byte("hello"))
	req = authedReq(req, imageOwnerSub)

	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for text/plain, got %d", w.Code)
	}
}

func TestGetUserImages_Success(t *testing.T) {
	fx := newImageFixture(t)
	// Seed two images for the owner via the upload handler.
	for i := 0; i < 2; i++ {
		req := buildMultipartRequest(t, "file", "cat.jpg", "image/jpeg", fakeJPEG())
		req = authedReq(req, imageOwnerSub)
		w := httptest.NewRecorder()
		fx.handler.UploadImage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d", i, w.Code)
		}
	}

	req := authedReq(httptest.NewRequest(http.MethodGet, "/v1/images", nil), imageOwnerSub)
	w := httptest.NewRecorder()
	fx.handler.GetUserImages(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp response.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if resp.Limit != 100 || resp.Offset != 0 {
		t.Errorf("expected limit=100 offset=0, got limit=%d offset=%d", resp.Limit, resp.Offset)
	}
}

func TestGetUserImages_RequiresAuth(t *testing.T) {
	fx := newImageFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/images", nil)
	w := httptest.NewRecorder()
	fx.handler.GetUserImages(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestDeleteImage_Success(t *testing.T) {
	fx := newImageFixture(t)
	created := uploadOne(t, fx, imageOwnerSub)

	req := httptest.NewRequest(http.MethodDelete, "/v1/images/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	req = authedReq(req, imageOwnerSub)
	w := httptest.NewRecorder()
	fx.handler.DeleteImage(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if len(fx.storage.deleted) != 1 {
		t.Errorf("expected exactly one storage delete, got %d", len(fx.storage.deleted))
	}
}

func TestDeleteImage_Forbidden(t *testing.T) {
	fx := newImageFixture(t)
	created := uploadOne(t, fx, imageOwnerSub)

	req := httptest.NewRequest(http.MethodDelete, "/v1/images/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	req = authedReq(req, imageOtherSub)
	w := httptest.NewRecorder()
	fx.handler.DeleteImage(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestDeleteImage_NotFound(t *testing.T) {
	fx := newImageFixture(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/images/"+missingImageID, nil)
	req.SetPathValue("id", missingImageID)
	req = authedReq(req, imageOwnerSub)
	w := httptest.NewRecorder()
	fx.handler.DeleteImage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestDeleteImage_RejectsNonULIDPath(t *testing.T) {
	fx := newImageFixture(t)
	const bad = "not-a-ulid"
	req := httptest.NewRequest(http.MethodDelete, "/v1/images/"+bad, nil)
	req.SetPathValue("id", bad)
	req = authedReq(req, imageOwnerSub)
	w := httptest.NewRecorder()
	fx.handler.DeleteImage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteImage_RequiresAuth(t *testing.T) {
	fx := newImageFixture(t)
	created := uploadOne(t, fx, imageOwnerSub)
	req := httptest.NewRequest(http.MethodDelete, "/v1/images/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	w := httptest.NewRecorder()
	fx.handler.DeleteImage(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// uploadOne posts a single image as ownerSub through the handler and
// returns the parsed response body. Tests use it to obtain a real ID
// they can then DELETE.
func uploadOne(t *testing.T, fx *imageFixture, ownerSub string) publicImageView {
	t.Helper()
	req := buildMultipartRequest(t, "file", "cat.jpg", "image/jpeg", fakeJPEG())
	req = authedReq(req, ownerSub)
	w := httptest.NewRecorder()
	fx.handler.UploadImage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed upload failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data publicImageView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp.Data
}

