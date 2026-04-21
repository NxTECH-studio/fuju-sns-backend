//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

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
			posts,
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
		Users: postgres.NewUserRepository(integrationPool),
		Posts: postgres.NewPostRepository(integrationPool),
		Likes: postgres.NewLikeRepository(integrationPool),
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
