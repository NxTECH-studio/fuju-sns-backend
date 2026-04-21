package ogp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/inmemory"
	"github.com/fuju/backend/pkg/ogp"
)

const enqueueTestUserID = "01OGPUSER0000000000000000Y"

// TestEnqueueAndWorker_MultiURL_AttachesAtReservedPositions is the end-to-end
// check for the multi-URL regression: a post containing two URLs must end up
// with both previews attached, one at position=0 and the other at position=1,
// regardless of the order the worker processes the jobs.
//
// Before the fix, both jobs attached at position=0 and the second write was
// silently dropped by the UNIQUE(post_id, position) constraint.
func TestEnqueueAndWorker_MultiURL_AttachesAtReservedPositions(t *testing.T) {
	// Serve distinct OGP titles per path so we can assert which URL landed
	// at which position.
	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:title" content="Alpha"></head></html>`))
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:title" content="Bravo"></head></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	links := inmemory.NewLinkStore()
	posts := inmemory.NewPostRepository(links)
	cache := inmemory.NewOGPCacheRepository(links)
	queue := inmemory.NewOGPJobQueue()

	post := &domain.Post{
		ID:      "01OGPPOSTMULTIURL0000000X0",
		UserID:  enqueueTestUserID,
		Content: fmt.Sprintf("check %s/a and also %s/b", srv.URL, srv.URL),
	}
	if _, err := posts.Create(context.Background(), post, nil, nil); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	fetcher := ogp.NewFetcher(&ogp.Options{
		Timeout: 2 * time.Second,
		Client:  srv.Client(),
	})
	enq := NewEnqueuer(cache, queue, posts, nil)

	enq.EnqueueForPost(context.Background(), post)

	// Drain the queue. Two URLs, two misses, two jobs expected.
	worker := NewWorker(queue, cache, posts, fetcher, nil)
	worker.interval = 0
	for i := 0; i < 2; i++ {
		worker.runOnce(context.Background())
	}

	// Both URLs should have been fetched exactly once.
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected 2 origin hits, got %d", got)
	}

	previews, err := cache.ListByPostIDs(context.Background(), []string{post.ID})
	if err != nil {
		t.Fatalf("list previews: %v", err)
	}
	attached := previews[post.ID]
	if len(attached) != 2 {
		t.Fatalf("expected 2 attached previews, got %d: %+v", len(attached), attached)
	}

	// ListByPostIDs returns rows ordered by position ascending, so the
	// first URL in the content must be at slot 0 and the second at slot 1.
	if attached[0].Title != "Alpha" {
		t.Errorf("position 0 title = %q, want Alpha", attached[0].Title)
	}
	if attached[1].Title != "Bravo" {
		t.Errorf("position 1 title = %q, want Bravo", attached[1].Title)
	}
}

// TestEnqueuer_MultiURL_EnqueuesPositionedJobs isolates the enqueuer: with
// two cache-miss URLs, two jobs must be created and carry position=0 and
// position=1 respectively.
func TestEnqueuer_MultiURL_EnqueuesPositionedJobs(t *testing.T) {
	links := inmemory.NewLinkStore()
	posts := inmemory.NewPostRepository(links)
	cache := inmemory.NewOGPCacheRepository(links)
	queue := inmemory.NewOGPJobQueue()

	post := &domain.Post{
		ID:      "01OGPPOSTMULTIURL0000000X1",
		UserID:  enqueueTestUserID,
		Content: "see https://example.com/first and https://example.net/second",
	}
	if _, err := posts.Create(context.Background(), post, nil, nil); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	enq := NewEnqueuer(cache, queue, posts, nil)
	enq.EnqueueForPost(context.Background(), post)

	// Claim both jobs and verify their Positions cover {0, 1} exactly.
	seen := map[int]string{}
	for i := 0; i < 2; i++ {
		job, err := queue.Claim(context.Background(), "test")
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if job == nil {
			t.Fatalf("expected 2 jobs, got %d", len(seen))
		}
		if _, dup := seen[job.Position]; dup {
			t.Fatalf("duplicate Position %d across jobs", job.Position)
		}
		seen[job.Position] = job.URL
	}
	if _, ok := seen[0]; !ok {
		t.Errorf("missing Position=0 job; got %+v", seen)
	}
	if _, ok := seen[1]; !ok {
		t.Errorf("missing Position=1 job; got %+v", seen)
	}

	// Queue should now be empty.
	extra, _ := queue.Claim(context.Background(), "test")
	if extra != nil {
		t.Errorf("unexpected extra job: %+v", extra)
	}
}
