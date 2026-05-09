package image

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
	"github.com/fuju/backend/internal/repository/inmemory"
	apperrors "github.com/fuju/backend/pkg/errors"
)

const (
	testUserSub  = "01TESTUSER000000000000000A"
	otherUserSub = "01TESTUSER000000000000000B"
)

// fakeStorage is a hand-rolled domain.StorageService used by these
// tests. We avoid mocking frameworks so the test reads top-to-bottom
// and the assertion targets stay obvious.
type fakeStorage struct {
	mu sync.Mutex

	// objects records every successful Upload as storageKey -> bytes.
	objects map[string][]byte
	// deleted records every Delete call (whether or not the key exists).
	deleted []string

	// nextStorageKey, nextPublicURL override the synthesized return
	// values when set; otherwise the fake derives stable defaults from
	// the request.
	nextStorageKey string
	nextPublicURL  string

	// uploadErr / deleteErr force an error from the next call when set.
	uploadErr error
	deleteErr error
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: make(map[string][]byte)}
}

func (s *fakeStorage) Upload(_ context.Context, req *domain.UploadImageRequest) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.uploadErr != nil {
		return "", "", s.uploadErr
	}
	storageKey := s.nextStorageKey
	if storageKey == "" {
		storageKey = "images/" + req.UserID + "/key/" + req.FileName
	}
	publicURL := s.nextPublicURL
	if publicURL == "" {
		publicURL = "https://cdn.example.com/" + storageKey
	}

	cp := make([]byte, len(req.FileData))
	copy(cp, req.FileData)
	s.objects[storageKey] = cp
	return storageKey, publicURL, nil
}

func (s *fakeStorage) Delete(_ context.Context, storageKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, storageKey)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, storageKey)
	return nil
}

func (s *fakeStorage) GetPublicURL(storageKey string) string {
	return "https://cdn.example.com/" + storageKey
}

// newLinkStore returns a fresh shared LinkStore. LinkStore is shared
// between PostRepository and ImageRepository in production. For the
// image usecase tests we don't need any post links, but the inmemory
// ImageRepository constructor requires one.
func newLinkStore() *inmemory.LinkStore {
	return inmemory.NewLinkStore()
}

func TestUploadImage_Success(t *testing.T) {
	storage := newFakeStorage()
	repo := inmemory.NewImageRepository(newLinkStore())
	uc := NewUploadImageUseCase(repo, storage)

	req := &domain.UploadImageRequest{
		FileName: "cat.jpg",
		FileData: []byte("\xff\xd8\xff\xe0fake-jpeg"),
		MimeType: "image/jpeg",
		UserID:   testUserSub,
	}

	got, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil image")
	}
	if got.UserID != testUserSub {
		t.Errorf("UserID: got %q, want %q", got.UserID, testUserSub)
	}
	if got.FileName != "cat.jpg" {
		t.Errorf("FileName: got %q, want cat.jpg", got.FileName)
	}
	if got.MimeType != "image/jpeg" {
		t.Errorf("MimeType: got %q, want image/jpeg", got.MimeType)
	}
	if got.FileSize != int64(len(req.FileData)) {
		t.Errorf("FileSize: got %d, want %d", got.FileSize, len(req.FileData))
	}
	if got.StorageKey == "" {
		t.Error("StorageKey should be populated from storage.Upload")
	}
	if got.PublicURL == "" {
		t.Error("PublicURL should be populated from storage.Upload")
	}
	if got.ID == "" {
		t.Error("ID should be a generated ULID")
	}

	// Round-trip via the repo to confirm the row is persisted with the
	// same StorageKey we received.
	stored, err := repo.GetByID(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored image to be retrievable")
	}
	if stored.StorageKey != got.StorageKey {
		t.Errorf("stored StorageKey mismatch: got %q, want %q", stored.StorageKey, got.StorageKey)
	}
}

func TestUploadImage_NilRequest(t *testing.T) {
	uc := NewUploadImageUseCase(inmemory.NewImageRepository(newLinkStore()), newFakeStorage())
	_, err := uc.Execute(context.Background(), nil)
	assertAppErrorCode(t, err, apperrors.ErrInvalidRequest)
}

func TestUploadImage_EmptyFileData(t *testing.T) {
	uc := NewUploadImageUseCase(inmemory.NewImageRepository(newLinkStore()), newFakeStorage())
	req := &domain.UploadImageRequest{
		FileName: "empty.jpg",
		FileData: []byte{},
		MimeType: "image/jpeg",
		UserID:   testUserSub,
	}
	_, err := uc.Execute(context.Background(), req)
	assertAppErrorCode(t, err, apperrors.ErrInvalidRequest)
}

func TestUploadImage_OverSizeLimit(t *testing.T) {
	uc := NewUploadImageUseCase(inmemory.NewImageRepository(newLinkStore()), newFakeStorage())
	// 5 MiB + 1 byte to trip the cap exactly. Allocation is intentional —
	// the usecase reads len(FileData) so we cannot fake the size with a
	// short slice.
	req := &domain.UploadImageRequest{
		FileName: "big.jpg",
		FileData: make([]byte, domain.MaxImageBytes+1),
		MimeType: "image/jpeg",
		UserID:   testUserSub,
	}
	_, err := uc.Execute(context.Background(), req)
	assertAppErrorCode(t, err, apperrors.ErrInvalidRequest)
}

func TestUploadImage_StorageError(t *testing.T) {
	storage := newFakeStorage()
	storage.uploadErr = errors.New("r2 unreachable")
	uc := NewUploadImageUseCase(inmemory.NewImageRepository(newLinkStore()), storage)

	req := &domain.UploadImageRequest{
		FileName: "x.jpg",
		FileData: []byte("data"),
		MimeType: "image/jpeg",
		UserID:   testUserSub,
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected storage error to propagate")
	}
	if !errors.Is(err, storage.uploadErr) {
		t.Errorf("expected storage error to be wrapped/returned, got %v", err)
	}
}

// TestUploadImage_RepoErrorLeavesOrphanInR2 documents the current
// behavior: when storage.Upload succeeds but repo.Create fails, the R2
// object is not rolled back. Out-of-scope for this task to fix; the
// test pins the behavior so a future refactor surfaces the change
// intentionally.
func TestUploadImage_RepoErrorReturnsDatabaseError(t *testing.T) {
	storage := newFakeStorage()
	repo := &errorImageRepo{createErr: errors.New("conn closed")}
	uc := NewUploadImageUseCase(repo, storage)

	req := &domain.UploadImageRequest{
		FileName: "x.jpg",
		FileData: []byte("data"),
		MimeType: "image/jpeg",
		UserID:   testUserSub,
	}
	_, err := uc.Execute(context.Background(), req)
	assertAppErrorCode(t, err, apperrors.ErrDatabaseError)

	// R2 object was uploaded but never deleted — orphan documented.
	if len(storage.objects) != 1 {
		t.Errorf("expected 1 orphaned R2 object, got %d", len(storage.objects))
	}
}

func TestGetUserImages_OnlyOwnImages(t *testing.T) {
	repo := inmemory.NewImageRepository(newLinkStore())
	storage := newFakeStorage()
	upload := NewUploadImageUseCase(repo, storage)

	for i, sub := range []string{testUserSub, testUserSub, otherUserSub} {
		req := &domain.UploadImageRequest{
			FileName: "f.jpg",
			FileData: []byte{byte(i)},
			MimeType: "image/jpeg",
			UserID:   sub,
		}
		if _, err := upload.Execute(context.Background(), req); err != nil {
			t.Fatalf("seed upload: %v", err)
		}
	}

	uc := NewGetUserImagesUseCase(repo)
	got, err := uc.Execute(context.Background(), testUserSub)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 images for testUserSub, got %d", len(got))
	}
	for _, img := range got {
		if img.UserID != testUserSub {
			t.Errorf("returned image owned by %q, want %q", img.UserID, testUserSub)
		}
	}
}

func TestGetUserImages_EmptyForUnknownUser(t *testing.T) {
	repo := inmemory.NewImageRepository(newLinkStore())
	uc := NewGetUserImagesUseCase(repo)

	got, err := uc.Execute(context.Background(), testUserSub)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d items", len(got))
	}
}

func TestGetUserImages_RejectsEmptySub(t *testing.T) {
	uc := NewGetUserImagesUseCase(inmemory.NewImageRepository(newLinkStore()))
	_, err := uc.Execute(context.Background(), "")
	assertAppErrorCode(t, err, apperrors.ErrInvalidRequest)
}

func TestDeleteImage_Success(t *testing.T) {
	repo := inmemory.NewImageRepository(newLinkStore())
	storage := newFakeStorage()

	created := seedImage(t, repo, storage, testUserSub)

	uc := NewDeleteImageUseCase(repo, storage, nil)
	if err := uc.Execute(context.Background(), created.ID, testUserSub); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got, err := repo.GetByID(context.Background(), created.ID); err != nil || got != nil {
		t.Errorf("expected soft-deleted image to be unreachable via GetByID, got %v err=%v", got, err)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != created.StorageKey {
		t.Errorf("expected single Delete on storage key %q, got %v", created.StorageKey, storage.deleted)
	}
}

func TestDeleteImage_Forbidden(t *testing.T) {
	repo := inmemory.NewImageRepository(newLinkStore())
	storage := newFakeStorage()
	created := seedImage(t, repo, storage, testUserSub)

	uc := NewDeleteImageUseCase(repo, storage, nil)
	err := uc.Execute(context.Background(), created.ID, otherUserSub)
	assertAppErrorCode(t, err, apperrors.ErrForbidden)

	if len(storage.deleted) != 0 {
		t.Errorf("expected no Delete on storage when forbidden, got %v", storage.deleted)
	}
}

func TestDeleteImage_NotFound(t *testing.T) {
	repo := inmemory.NewImageRepository(newLinkStore())
	uc := NewDeleteImageUseCase(repo, newFakeStorage(), nil)
	err := uc.Execute(context.Background(), "01TESTNOTFOUND0000000000A0", testUserSub)
	assertAppErrorCode(t, err, apperrors.ErrNotFound)
}

func TestDeleteImage_RejectsEmptyID(t *testing.T) {
	uc := NewDeleteImageUseCase(inmemory.NewImageRepository(newLinkStore()), newFakeStorage(), nil)
	err := uc.Execute(context.Background(), "", testUserSub)
	assertAppErrorCode(t, err, apperrors.ErrInvalidRequest)
}

func TestDeleteImage_StorageFailureStillSoftDeletes(t *testing.T) {
	// R2 delete is best-effort: even when the object delete fails the
	// DB row must be marked deleted_at so the user no longer sees it.
	repo := inmemory.NewImageRepository(newLinkStore())
	storage := newFakeStorage()
	created := seedImage(t, repo, storage, testUserSub)
	storage.deleteErr = errors.New("r2 unreachable")

	uc := NewDeleteImageUseCase(repo, storage, nil)
	if err := uc.Execute(context.Background(), created.ID, testUserSub); err != nil {
		t.Fatalf("Execute should swallow storage delete errors, got %v", err)
	}
	if got, _ := repo.GetByID(context.Background(), created.ID); got != nil {
		t.Errorf("expected DB row to be soft-deleted even on storage failure")
	}
}

// errorImageRepo lets us inject a Create failure without bringing in a
// full mock framework. Only the methods used by the tests are
// implemented; the rest panic so an accidental call surfaces clearly.
type errorImageRepo struct {
	createErr error
}

func (r *errorImageRepo) Create(_ context.Context, _ *domain.Image) (*domain.Image, error) {
	return nil, r.createErr
}
func (r *errorImageRepo) GetByID(_ context.Context, _ string) (*domain.Image, error) {
	panic("not used")
}
func (r *errorImageRepo) GetByUserID(_ context.Context, _ string) ([]*domain.Image, error) {
	panic("not used")
}
func (r *errorImageRepo) Delete(_ context.Context, _ string) error { panic("not used") }
func (r *errorImageRepo) ListByPostID(_ context.Context, _ string) ([]*domain.Image, error) {
	panic("not used")
}
func (r *errorImageRepo) ListByPostIDs(_ context.Context, _ []string) (map[string][]*domain.Image, error) {
	panic("not used")
}

// seedImage uploads a single image via the real Upload usecase so the
// stored row matches what production would persist.
func seedImage(t *testing.T, repo repository.ImageRepository, storage *fakeStorage, ownerSub string) *domain.Image {
	t.Helper()
	upload := NewUploadImageUseCase(repo, storage)
	req := &domain.UploadImageRequest{
		FileName: "seed.jpg",
		FileData: []byte("seed"),
		MimeType: "image/jpeg",
		UserID:   ownerSub,
	}
	img, err := upload.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	return img
}

func assertAppErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", wantCode)
	}
	appErr, ok := apperrors.IsAppError(err)
	if !ok {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("error code: got %q, want %q (msg=%s)", appErr.Code, wantCode, appErr.Message)
	}
}
