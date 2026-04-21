//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository/postgres"
	"github.com/fuju/backend/internal/repository/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationPool is the shared pgxpool used by every integration
// test in this package. TestMain populates it from DATABASE_URL;
// subtests share it, and newContract TRUNCATEs between each — so
// subtests MUST NOT call t.Parallel() lest one test's fixtures be
// wiped mid-flight by another's truncate.
var integrationPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("DB_TEST_DSN"))
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "integration tests require DATABASE_URL (postgres)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: pgxpool.New: %v\n", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "integration: ping: %v\n", err)
		os.Exit(1)
	}
	integrationPool = pool

	code := m.Run()

	pool.Close()
	os.Exit(code)
}

// truncateAll wipes every table the Phase 1 repositories touch. The
// ordering is irrelevant because CASCADE handles FK chains; we list
// the tables explicitly so future readers can see the surface area at
// a glance.
func truncateAll(t *testing.T) {
	t.Helper()
	_, err := integrationPool.Exec(context.Background(), `
		TRUNCATE TABLE
			likes,
			post_ogp,
			post_tags,
			post_images,
			ogp_jobs,
			ogp_cache,
			posts,
			follows,
			user_badges,
			badges,
			tags,
			images,
			users
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// newContract returns a truncated postgres-backed Contract. Because
// it mutates the shared integrationPool via TRUNCATE, callers (and
// every contract subtest) MUST remain serial. Do not introduce
// t.Parallel() into this package.
func newContract(t *testing.T) testsupport.Contract {
	truncateAll(t)
	return testsupport.Contract{
		Users:    postgres.NewUserRepository(integrationPool),
		Posts:    postgres.NewPostRepository(integrationPool),
		Likes:    postgres.NewLikeRepository(integrationPool),
		Follows:  postgres.NewFollowRepository(integrationPool),
		Tags:     postgres.NewTagRepository(integrationPool),
		Badges:   postgres.NewBadgeRepository(integrationPool),
		Images:   postgres.NewImageRepository(integrationPool),
		OGPCache: postgres.NewOGPCacheRepository(integrationPool),
		OGPJobs:  postgres.NewOGPJobQueue(integrationPool),
	}
}

func TestUserRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunUserRepositoryContract(t, newContract)
}

func TestPostRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunPostRepositoryContract(t, newContract)
}

func TestLikeRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunLikeRepositoryContract(t, newContract)
}

func TestFollowRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunFollowRepositoryContract(t, newContract)
}

func TestTagRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunTagRepositoryContract(t, newContract)
}

func TestBadgeRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunBadgeRepositoryContract(t, newContract)
}

func TestImageRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunImageRepositoryContract(t, newContract)
}

func TestOGPCacheRepository_Contract_Postgres(t *testing.T) {
	testsupport.RunOGPCacheRepositoryContract(t, newContract)
}

func TestOGPJobQueue_Contract_Postgres(t *testing.T) {
	testsupport.RunOGPJobQueueContract(t, newContract)
}

// TestOGPJobQueue_ConcurrentClaim_Postgres verifies the
// `FOR UPDATE SKIP LOCKED` invariant: two workers racing on Claim
// must always see distinct jobs. This property is inherent to the
// postgres implementation (inmemory serializes under a mutex) and
// cannot be exercised by the backend-agnostic contract runner.
func TestOGPJobQueue_ConcurrentClaim_Postgres(t *testing.T) {
	c := newContract(t)
	ctx := context.Background()

	// Seed a user + post + two queued jobs.
	if _, err := c.Users.Upsert(ctx, testUser("01HAAAAAAAAAAAAAAAAAAAAAAA")); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := c.Posts.Create(ctx, testPost("01HPAAAAAAAAAAAAAAAAAAAAAA", "01HAAAAAAAAAAAAAAAAAAAAAAA"), nil, nil); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	ids := []string{
		"01HJOBAAAAAAAAAAAAAAAAAAAA",
		"01HJOBBBBBBBBBBBBBBBBBBBBB",
	}
	for _, id := range ids {
		if err := c.OGPJobs.Enqueue(ctx, id,
			"00000000000000000000000000000000000000000000000000000000000abcd0",
			"https://example.com/"+id,
			"01HPAAAAAAAAAAAAAAAAAAAAAA", 0); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}

	// Launch two concurrent claim goroutines.
	type result struct {
		job *domain.OGPJob
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			j, err := c.OGPJobs.Claim(ctx, "worker")
			results <- result{j, err}
		}()
	}
	close(start)

	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("claim: %v", r.err)
		}
		if r.job == nil {
			t.Fatalf("expected a job, got nil")
		}
		if seen[r.job.ID] {
			t.Fatalf("SKIP LOCKED invariant violated: job %s claimed twice", r.job.ID)
		}
		seen[r.job.ID] = true
	}
	if len(seen) != 2 {
		t.Errorf("expected 2 distinct jobs claimed, got %d", len(seen))
	}
}

// TestAttachOGP_Postgres covers PostRepository.AttachOGP in isolation
// because the contract runner cannot satisfy the post_ogp → ogp_cache
// FK without knowledge of the OGPCacheRepository (landing in Phase 3).
func TestAttachOGP_Postgres(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	// Seed a user + post as FK targets.
	users := postgres.NewUserRepository(integrationPool)
	posts := postgres.NewPostRepository(integrationPool)
	seedSub := "01HAAAAAAAAAAAAAAAAAAAAAAA"
	seedPost := "01HPAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := users.Upsert(ctx, testUser(seedSub)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := posts.Create(ctx, testPost(seedPost, seedSub), nil, nil); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	// Pre-seed ogp_cache rows directly via the pool so AttachOGP's
	// FK check resolves.
	hash1 := "00000000000000000000000000000000000000000000000000000000000abcd0"
	hash2 := "00000000000000000000000000000000000000000000000000000000000abcd1"
	expires := time.Now().Add(24 * time.Hour)
	for _, h := range []string{hash1, hash2} {
		if _, err := integrationPool.Exec(ctx, `
			INSERT INTO ogp_cache (url_hash, url, expires_at)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, h, "https://example.com/"+h, expires); err != nil {
			t.Fatalf("seed cache %s: %v", h, err)
		}
	}

	// First attach at position 0 should land.
	if err := posts.AttachOGP(ctx, seedPost, hash1, 0); err != nil {
		t.Fatalf("attach #1: %v", err)
	}
	// Re-attach same (post, hash, position) — idempotent via PK conflict.
	if err := posts.AttachOGP(ctx, seedPost, hash1, 0); err != nil {
		t.Fatalf("attach #2 (dup): %v", err)
	}
	// Attach different hash at the same position — idempotent via
	// UNIQUE(post_id, position) conflict, first writer wins.
	if err := posts.AttachOGP(ctx, seedPost, hash2, 0); err != nil {
		t.Fatalf("attach #3 (pos conflict): %v", err)
	}

	// Only the first writer should survive.
	var gotHash string
	if err := integrationPool.QueryRow(ctx,
		`SELECT url_hash FROM post_ogp WHERE post_id = $1 AND position = 0`, seedPost,
	).Scan(&gotHash); err != nil {
		t.Fatalf("post_ogp select: %v", err)
	}
	if gotHash != hash1 {
		t.Errorf("expected first-writer hash %s, got %s", hash1, gotHash)
	}
}
