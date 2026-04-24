package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
)

// OGP cache / job fixtures. Hashes are 64-char hex to match the
// CHAR(64) url_hash column; job IDs are ULIDs sorted lexicographically
// so FIFO assertions line up.
const (
	ogpHashA = "00000000000000000000000000000000000000000000000000000000000abcd0"
	ogpHashB = "00000000000000000000000000000000000000000000000000000000000abcd1"

	jobA = "01HJOBAAAAAAAAAAAAAAAAAAAA"
	jobB = "01HJOBBBBBBBBBBBBBBBBBBBBB"
	jobC = "01HJOBCCCCCCCCCCCCCCCCCCCC"

	ogpTitleUpdated = "updated"
)

func seedOGPPreview(t *testing.T, cache repository.OGPCacheRepository, urlHash, url string) {
	t.Helper()
	err := cache.Upsert(context.Background(), &domain.OGPPreview{
		URLHash:   urlHash,
		URL:       url,
		Title:     "title-" + urlHash[:8],
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Status:    domain.OGPStatusOK,
	})
	if err != nil {
		t.Fatalf("seed ogp preview %s: %v", urlHash, err)
	}
}

// RunOGPCacheRepositoryContract exercises OGPCacheRepository.
// ListByPostIDs requires post_ogp rows, which flow through
// PostRepository.AttachOGP — so Users / Posts / OGPCache must all be
// populated in the Contract bundle.
func RunOGPCacheRepositoryContract(t *testing.T, newContract Factory) {
	t.Helper()

	t.Run("Get_missing_returns_nil", func(t *testing.T) {
		c := newContract(t)
		got, err := c.OGPCache.Get(context.Background(), ogpHashA)
		if err != nil {
			t.Fatalf("get miss: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil on miss, got %+v", got)
		}
	})

	t.Run("Upsert_insert_then_Get", func(t *testing.T) {
		c := newContract(t)
		seedOGPPreview(t, c.OGPCache, ogpHashA, "https://example.com/a")

		got, err := c.OGPCache.Get(context.Background(), ogpHashA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got == nil || got.URL != "https://example.com/a" {
			t.Fatalf("unexpected: %+v", got)
		}
		if got.ExpiresAt.IsZero() {
			t.Errorf("expected expires_at populated")
		}
	})

	t.Run("Upsert_update_overwrites_mutable_fields", func(t *testing.T) {
		c := newContract(t)
		seedOGPPreview(t, c.OGPCache, ogpHashA, "https://example.com/v1")

		// Re-upsert with new title / URL — should take effect.
		err := c.OGPCache.Upsert(context.Background(), &domain.OGPPreview{
			URLHash:   ogpHashA,
			URL:       "https://example.com/v2",
			Title:     ogpTitleUpdated,
			ExpiresAt: time.Now().Add(time.Hour),
			Status:    domain.OGPStatusOK,
		})
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		got, err := c.OGPCache.Get(context.Background(), ogpHashA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got == nil || got.Title != ogpTitleUpdated || got.URL != "https://example.com/v2" {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("ListByPostIDs_orders_by_position", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "x", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("post A: %v", err)
		}
		// postB has no OGP attached.
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postB, UserID: userA, Content: "y", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("post B: %v", err)
		}

		seedOGPPreview(t, c.OGPCache, ogpHashA, "https://example.com/a")
		seedOGPPreview(t, c.OGPCache, ogpHashB, "https://example.com/b")

		// Attach in reverse position order to verify sort.
		if err := c.Posts.AttachOGP(context.Background(), postA, ogpHashB, 1); err != nil {
			t.Fatalf("attach 1: %v", err)
		}
		if err := c.Posts.AttachOGP(context.Background(), postA, ogpHashA, 0); err != nil {
			t.Fatalf("attach 0: %v", err)
		}

		got, err := c.OGPCache.ListByPostIDs(context.Background(), []string{postA, postB})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 post with OGP (postB absent), got %d (%+v)", len(got), got)
		}
		if len(got[postA]) != 2 {
			t.Fatalf("expected 2 previews for postA, got %d", len(got[postA]))
		}
		if got[postA][0].URLHash != ogpHashA || got[postA][1].URLHash != ogpHashB {
			t.Errorf("expected position 0 (hashA) then position 1 (hashB), got %+v", got[postA])
		}
	})
}

// RunOGPJobQueueContract exercises OGPJobQueue. The queue references
// posts(id) via FK on post_id, so Users + Posts must be populated.
func RunOGPJobQueueContract(t *testing.T, newContract Factory) {
	t.Helper()

	seed := func(t *testing.T, c Contract) {
		t.Helper()
		seedUser(t, c.Users, userA)
		if _, err := c.Posts.Create(context.Background(), &domain.Post{
			ID: postA, UserID: userA, Content: "x", Visibility: "public",
		}, nil, nil); err != nil {
			t.Fatalf("seed post: %v", err)
		}
	}

	t.Run("Claim_empty_returns_nil", func(t *testing.T) {
		c := newContract(t)
		got, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil on empty queue, got %+v", got)
		}
	})

	t.Run("Enqueue_then_Claim_transitions_to_running", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue: %v", err)
		}

		j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if j == nil {
			t.Fatalf("expected job, got nil")
		}
		if j.ID != jobA {
			t.Errorf("unexpected id: %s", j.ID)
		}
		if j.Status != domain.OGPJobRunning {
			t.Errorf("expected status=running, got %s", j.Status)
		}
		if j.Attempts != 1 {
			t.Errorf("expected attempts=1, got %d", j.Attempts)
		}
		if j.StartedAt == nil {
			t.Errorf("expected started_at set")
		}
	})

	t.Run("Claim_FIFO_by_enqueued_at", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)

		for _, id := range []string{jobA, jobB, jobC} {
			if err := c.OGPJobs.Enqueue(context.Background(), id, ogpHashA, "https://example.com/"+id, postA, 0); err != nil {
				t.Fatalf("enqueue %s: %v", id, err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		var order []string
		for i := 0; i < 3; i++ {
			j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
			if err != nil {
				t.Fatalf("claim %d: %v", i, err)
			}
			if j == nil {
				t.Fatalf("claim %d returned nil", i)
			}
			order = append(order, j.ID)
		}
		if order[0] != jobA || order[1] != jobB || order[2] != jobC {
			t.Errorf("expected FIFO order [A,B,C], got %v", order)
		}
	})

	t.Run("MarkDone_finalizes", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if _, err := c.OGPJobs.Claim(context.Background(), "worker-1"); err != nil {
			t.Fatalf("claim: %v", err)
		}
		if err := c.OGPJobs.MarkDone(context.Background(), jobA); err != nil {
			t.Fatalf("done: %v", err)
		}
		// After done, queue is empty and re-claim returns nil.
		j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		if j != nil {
			t.Fatalf("expected empty queue, got %+v", j)
		}
	})

	t.Run("MarkFailed_retriable_requeues", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if _, err := c.OGPJobs.Claim(context.Background(), "worker-1"); err != nil {
			t.Fatalf("claim: %v", err)
		}
		if err := c.OGPJobs.MarkFailed(context.Background(), jobA, "transient", true); err != nil {
			t.Fatalf("mark failed: %v", err)
		}

		// Retriable → queued again, reclaim succeeds with attempts=2.
		j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		if j == nil {
			t.Fatalf("expected reclaim")
		}
		if j.Attempts != 2 {
			t.Errorf("expected attempts=2 on retry, got %d", j.Attempts)
		}
		if j.LastError != "transient" {
			t.Errorf("expected last_error preserved, got %q", j.LastError)
		}
	})

	t.Run("MarkFailed_terminal_finalizes", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if _, err := c.OGPJobs.Claim(context.Background(), "worker-1"); err != nil {
			t.Fatalf("claim: %v", err)
		}
		if err := c.OGPJobs.MarkFailed(context.Background(), jobA, "permanent", false); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
		j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		if j != nil {
			t.Fatalf("expected queue empty (failed is terminal), got %+v", j)
		}
	})

	t.Run("Enqueue_duplicate_id_is_noop", func(t *testing.T) {
		c := newContract(t)
		seed(t, c)
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue 1: %v", err)
		}
		if err := c.OGPJobs.Enqueue(context.Background(), jobA, ogpHashA, "https://example.com/a", postA, 0); err != nil {
			t.Fatalf("enqueue 2 (dup): %v", err)
		}

		// Only one job should come out of the queue.
		if _, err := c.OGPJobs.Claim(context.Background(), "worker-1"); err != nil {
			t.Fatalf("claim 1: %v", err)
		}
		j, err := c.OGPJobs.Claim(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("claim 2: %v", err)
		}
		if j != nil {
			t.Fatalf("expected empty after single dedup, got %+v", j)
		}
	})
}
