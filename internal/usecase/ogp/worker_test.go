package ogp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/pkg/ogp"
	"github.com/oklog/ulid/v2"
)

const testPostID = "01OGPPOSTID00000000000000X"

func newWorkerFixture(t *testing.T, handler http.Handler) (*Worker, *httptest.Server, OGPJobQueueLike) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	links := inmemory.NewLinkStore()
	postRepo := inmemory.NewPostRepository(links)
	cache := inmemory.NewOGPCacheRepository(links)
	queue := inmemory.NewOGPJobQueue()

	// Seed the referenced post so AttachOGP doesn't race against a
	// missing post row in later checks. In-memory AttachOGP is link-
	// store only, so it doesn't actually require the post to exist,
	// but the assertion below does.
	_, err := postRepo.Create(context.Background(), &domain.Post{
		ID:      testPostID,
		UserID:  "01OGPUSER0000000000000000X",
		Content: "hi",
	}, nil, nil)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}

	fetcher := ogp.NewFetcher(&ogp.Options{
		Timeout: 2 * time.Second,
		Client:  srv.Client(),
	})

	w := NewWorker(queue, cache, postRepo, fetcher, nil)
	w.interval = 0 // no sleep between Claim misses
	return w, srv, queue
}

// OGPJobQueueLike is the subset of the repository interface the tests
// use. Declared here so the test file doesn't have to import the full
// repository package.
type OGPJobQueueLike interface {
	Enqueue(ctx context.Context, id, urlHash, url, postID string) error
	Claim(ctx context.Context, workerID string) (*domain.OGPJob, error)
	MarkDone(ctx context.Context, jobID string) error
	MarkFailed(ctx context.Context, jobID, reason string, retriable bool) error
}

func TestWorker_Success_CachesAndMarksDone(t *testing.T) {
	w, srv, queue := newWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:title" content="Hello"></head></html>`))
	}))

	target, _ := ogp.Normalize(srv.URL + "/article")
	hash := ogp.Hash(target)
	jobID := ulid.Make().String()
	if err := queue.Enqueue(context.Background(), jobID, hash, target, testPostID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	w.runOnce(context.Background())

	// Cache row must exist with status=ok and 3-day TTL.
	cache := w.cache
	row, _ := cache.Get(context.Background(), hash)
	if row == nil {
		t.Fatal("expected cache row")
	}
	if row.Status != domain.OGPStatusOK {
		t.Errorf("status = %q", row.Status)
	}
	if row.Title != "Hello" {
		t.Errorf("title = %q", row.Title)
	}
	// ExpiresAt is set relative to the worker's now(), which may be a
	// few nanoseconds after the fetcher's FetchedAt — compare with a
	// generous tolerance instead of equality.
	if ttl := time.Until(row.ExpiresAt); ttl < SuccessTTL-time.Second || ttl > SuccessTTL+time.Second {
		t.Errorf("ExpiresAt not ~SuccessTTL ahead: got %v (ttl %v)", row.ExpiresAt, ttl)
	}
}

func TestWorker_5xxRetriable_LeavesQueued(t *testing.T) {
	w, srv, queue := newWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	target, _ := ogp.Normalize(srv.URL + "/")
	hash := ogp.Hash(target)
	jobID := ulid.Make().String()
	_ = queue.Enqueue(context.Background(), jobID, hash, target, testPostID)

	w.runOnce(context.Background())

	// No cache row should have been written on a retriable failure.
	row, _ := w.cache.Get(context.Background(), hash)
	if row != nil {
		t.Errorf("expected no cache row on retriable failure, got %+v", row)
	}
	// Job should be back in queued state.
	claimed, _ := queue.Claim(context.Background(), "test")
	if claimed == nil || claimed.ID != jobID {
		t.Fatalf("expected job to be re-queued; claimed=%v", claimed)
	}
}

func TestWorker_4xxNonRetriable_WritesErrorRow(t *testing.T) {
	w, srv, queue := newWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	target, _ := ogp.Normalize(srv.URL + "/missing")
	hash := ogp.Hash(target)
	jobID := ulid.Make().String()
	_ = queue.Enqueue(context.Background(), jobID, hash, target, testPostID)

	w.runOnce(context.Background())

	row, _ := w.cache.Get(context.Background(), hash)
	if row == nil {
		t.Fatal("expected error cache row on non-retriable failure")
	}
	if row.Status != domain.OGPStatusError {
		t.Errorf("status = %q, want error", row.Status)
	}
	if ttl := time.Until(row.ExpiresAt); ttl < ErrorTTL-time.Second || ttl > ErrorTTL+time.Second {
		t.Errorf("error TTL drift: got %v (ttl %v)", row.ExpiresAt, ttl)
	}
}
