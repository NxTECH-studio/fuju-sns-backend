// Package testsupport contains test helpers shared across repository
// implementations. The exported Run*Contract functions assert the
// behavioural contract of repository.* interfaces and are invoked from
// both the in-memory and postgres test packages. Driving the same
// assertions from both backends catches drift as soon as an
// implementation diverges from the interface contract.
package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/fuju/backend/internal/domain"
	"github.com/fuju/backend/internal/repository"
)

// Contract bundles every repository an implementation wants to cover
// under contract tests. Individual Run*Contract functions dereference
// only the fields they need — backends that do not yet implement a
// repo leave the field nil.
type Contract struct {
	Users repository.UserRepository
	Posts repository.PostRepository
	Likes repository.LikeRepository
}

// Factory is a fresh-state constructor: each subtest calls it to get
// a clean repository bundle. Postgres factories TRUNCATE the relevant
// tables; in-memory factories allocate new maps.
type Factory func(t *testing.T) Contract

// Stable ULIDs for fixtures. Crockford Base32 excludes I, L, O, U, so
// the body uses only A/B/C/P/Z which sort lexically in the order the
// tests expect.
const (
	userA = "01HAAAAAAAAAAAAAAAAAAAAAAA"
	userB = "01HABBBBBBBBBBBBBBBBBBBBBB"
	userC = "01HCCCCCCCCCCCCCCCCCCCCCCC"

	postA = "01HPAAAAAAAAAAAAAAAAAAAAAA"
	postB = "01HPBBBBBBBBBBBBBBBBBBBBBB"
	postC = "01HPCCCCCCCCCCCCCCCCCCCCCC"

	missingID = "01HZZZZZZZZZZZZZZZZZZZZZZZ"
)

func seedUser(t *testing.T, users repository.UserRepository, sub string) {
	t.Helper()
	_, err := users.Upsert(context.Background(), &domain.User{
		Sub:                sub,
		DisplayNameCached:  sub,
		DisplayIDCached:    sub,
		IconURLCached:      "",
		ProfileRefreshedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed user %s: %v", sub, err)
	}
}

// RunUserRepositoryContract exercises the UserRepository interface.
func RunUserRepositoryContract(t *testing.T, newContract Factory) {
	t.Helper()

	t.Run("GetBySub_miss_returns_nil", func(t *testing.T) {
		c := newContract(t)
		got, err := c.Users.GetBySub(context.Background(), missingID)
		if err != nil {
			t.Fatalf("GetBySub: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil on miss, got %+v", got)
		}
	})

	t.Run("Upsert_insert_then_GetBySub", func(t *testing.T) {
		c := newContract(t)
		u := &domain.User{
			Sub:                userA,
			DisplayNameCached:  "alice",
			DisplayIDCached:    "alice",
			IconURLCached:      "https://cdn/alice.png",
			ProfileRefreshedAt: time.Now().UTC(),
			Bio:                "hello",
			BannerURL:          "https://cdn/banner.png",
		}
		got, err := c.Users.Upsert(context.Background(), u)
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got == nil || got.Sub != userA {
			t.Fatalf("Upsert returned unexpected: %+v", got)
		}

		re, err := c.Users.GetBySub(context.Background(), userA)
		if err != nil {
			t.Fatalf("GetBySub: %v", err)
		}
		if re == nil {
			t.Fatalf("GetBySub returned nil")
		}
		if re.DisplayNameCached != "alice" || re.Bio != "hello" || re.BannerURL != "https://cdn/banner.png" {
			t.Fatalf("stored fields wrong: %+v", re)
		}
	})

	t.Run("Upsert_update_preserves_is_admin_counters_created_at", func(t *testing.T) {
		c := newContract(t)
		// Seed an admin user with counters and an explicit createdAt.
		orig := &domain.User{
			Sub:                userA,
			DisplayNameCached:  "alice",
			DisplayIDCached:    "alice",
			ProfileRefreshedAt: time.Now().UTC(),
			IsAdmin:            true,
			FollowersCount:     5,
			FollowingCount:     7,
			Bio:                "original bio",
			BannerURL:          "https://orig/banner.png",
		}
		seeded, err := c.Users.Upsert(context.Background(), orig)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		seededAt := seeded.CreatedAt

		// Re-upsert with zeroed server-owned fields + empty bio/banner.
		refresh := &domain.User{
			Sub:                userA,
			DisplayNameCached:  "alice.v2",
			DisplayIDCached:    "alice",
			ProfileRefreshedAt: time.Now().UTC(),
			IsAdmin:            false, // caller must not flip this off
			FollowersCount:     0,     // ditto
			FollowingCount:     0,
			// Bio and BannerURL left empty; repository must preserve existing.
		}
		out, err := c.Users.Upsert(context.Background(), refresh)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if !out.IsAdmin {
			t.Errorf("expected IsAdmin preserved true, got false")
		}
		if out.FollowersCount != 5 || out.FollowingCount != 7 {
			t.Errorf("expected counters preserved (5,7), got (%d,%d)", out.FollowersCount, out.FollowingCount)
		}
		if out.Bio != "original bio" {
			t.Errorf("expected bio preserved, got %q", out.Bio)
		}
		if out.BannerURL != "https://orig/banner.png" {
			t.Errorf("expected banner preserved, got %q", out.BannerURL)
		}
		if !out.CreatedAt.Equal(seededAt) {
			t.Errorf("expected CreatedAt preserved (%v), got %v", seededAt, out.CreatedAt)
		}
		if out.DisplayNameCached != "alice.v2" {
			t.Errorf("expected cached name updated, got %q", out.DisplayNameCached)
		}
	})

	t.Run("UpdateProfile_applies_non_nil_fields", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		newBio := "updated bio"
		out, err := c.Users.UpdateProfile(context.Background(), userA, &domain.UpdateUserProfileRequest{
			Bio: &newBio,
		})
		if err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		if out == nil || out.Bio != "updated bio" {
			t.Fatalf("expected bio updated, got %+v", out)
		}
	})

	t.Run("UpdateProfile_missing_returns_nil", func(t *testing.T) {
		c := newContract(t)
		b := "x"
		out, err := c.Users.UpdateProfile(context.Background(), missingID, &domain.UpdateUserProfileRequest{Bio: &b})
		if err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		if out != nil {
			t.Fatalf("expected nil on miss, got %+v", out)
		}
	})

	t.Run("UpdateProfile_nil_field_preserves_existing", func(t *testing.T) {
		c := newContract(t)
		seed := &domain.User{
			Sub:                userA,
			DisplayNameCached:  "alice",
			DisplayIDCached:    "alice",
			ProfileRefreshedAt: time.Now().UTC(),
			Bio:                "keep me",
			BannerURL:          "https://keep/me.png",
		}
		if _, err := c.Users.Upsert(context.Background(), seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		newBio := "updated"
		// Only Bio is supplied; BannerURL is nil and must be preserved.
		out, err := c.Users.UpdateProfile(context.Background(), userA, &domain.UpdateUserProfileRequest{Bio: &newBio})
		if err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		if out == nil {
			t.Fatalf("expected row, got nil")
		}
		if out.Bio != "updated" {
			t.Errorf("bio not updated: %q", out.Bio)
		}
		if out.BannerURL != "https://keep/me.png" {
			t.Errorf("banner not preserved: %q", out.BannerURL)
		}
	})

	t.Run("UpdateProfile_explicit_empty_string_clears", func(t *testing.T) {
		c := newContract(t)
		seed := &domain.User{
			Sub:                userA,
			DisplayNameCached:  "alice",
			DisplayIDCached:    "alice",
			ProfileRefreshedAt: time.Now().UTC(),
			Bio:                "old bio",
		}
		if _, err := c.Users.Upsert(context.Background(), seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		empty := ""
		out, err := c.Users.UpdateProfile(context.Background(), userA, &domain.UpdateUserProfileRequest{Bio: &empty})
		if err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		if out == nil || out.Bio != "" {
			t.Fatalf("expected bio cleared, got %+v", out)
		}
	})

	t.Run("List_skips_soft_deleted_and_reports_total", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedUser(t, c.Users, userB)
		seedUser(t, c.Users, userC)

		if err := c.Users.Delete(context.Background(), userC); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		list, total, err := c.Users.List(context.Background(), 100, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total=2 live users, got %d", total)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 list entries, got %d", len(list))
		}
		for _, u := range list {
			if u.Sub == userC {
				t.Errorf("soft-deleted user leaked into list: %+v", u)
			}
		}
	})

	t.Run("ListBySubs_batches_and_drops_missing", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedUser(t, c.Users, userB)

		got, err := c.Users.ListBySubs(context.Background(), []string{userA, userB, missingID})
		if err != nil {
			t.Fatalf("ListBySubs: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("expected 2 entries, got %d", len(got))
		}
		if _, ok := got[userA]; !ok {
			t.Errorf("expected %s present", userA)
		}
		if _, ok := got[userB]; !ok {
			t.Errorf("expected %s present", userB)
		}
	})

	t.Run("Counter_mutators_floor_at_zero", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		if err := c.Users.IncrementFollowersCount(context.Background(), userA); err != nil {
			t.Fatalf("inc followers: %v", err)
		}
		if err := c.Users.IncrementFollowersCount(context.Background(), userA); err != nil {
			t.Fatalf("inc followers: %v", err)
		}
		if err := c.Users.DecrementFollowersCount(context.Background(), userA); err != nil {
			t.Fatalf("dec followers: %v", err)
		}
		if err := c.Users.IncrementFollowingCount(context.Background(), userA); err != nil {
			t.Fatalf("inc following: %v", err)
		}

		u, err := c.Users.GetBySub(context.Background(), userA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if u.FollowersCount != 1 || u.FollowingCount != 1 {
			t.Fatalf("expected followers=1 following=1, got %d / %d", u.FollowersCount, u.FollowingCount)
		}

		// Decrement past zero should floor, not go negative.
		if err := c.Users.DecrementFollowersCount(context.Background(), userA); err != nil {
			t.Fatalf("dec followers: %v", err)
		}
		if err := c.Users.DecrementFollowersCount(context.Background(), userA); err != nil {
			t.Fatalf("dec followers (past zero): %v", err)
		}
		u, err = c.Users.GetBySub(context.Background(), userA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if u.FollowersCount != 0 {
			t.Errorf("expected followers=0 after flooring, got %d", u.FollowersCount)
		}
	})

	t.Run("Delete_soft_deletes_but_GetBySub_still_returns_row", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		if err := c.Users.Delete(context.Background(), userA); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		// Row still retrievable by sub: soft-delete is transparent to
		// GetBySub per the interface contract, so handlers can decide
		// whether to surface deleted users (e.g. for "@user was
		// removed" placeholders). List / ListBySubs however filter.
		got, err := c.Users.GetBySub(context.Background(), userA)
		if err != nil {
			t.Fatalf("GetBySub: %v", err)
		}
		if got == nil {
			t.Fatalf("expected GetBySub to return soft-deleted row, got nil")
		}
		if got.DeletedAt == nil {
			t.Errorf("expected DeletedAt populated on returned row, got nil")
		}

		_, total, err := c.Users.List(context.Background(), 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 0 {
			t.Errorf("expected List total=0 after soft-delete, got %d", total)
		}

		bysubs, err := c.Users.ListBySubs(context.Background(), []string{userA})
		if err != nil {
			t.Fatalf("ListBySubs: %v", err)
		}
		if _, present := bysubs[userA]; present {
			t.Errorf("expected soft-deleted user absent from ListBySubs, got present")
		}
	})
}

// RunPostRepositoryContract exercises the PostRepository interface. It
// requires a UserRepository in the Contract because post rows reference
// users via FK on the postgres backend.
func RunPostRepositoryContract(t *testing.T, newContract Factory) {
	t.Helper()

	t.Run("Create_then_GetByID", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		post := &domain.Post{
			ID:         postA,
			UserID:     userA,
			Content:    "hello world",
			Visibility: "public",
		}
		out, err := c.Posts.Create(context.Background(), post, nil, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if out == nil || out.ID != postA {
			t.Fatalf("Create returned unexpected: %+v", out)
		}
		if out.CreatedAt.IsZero() || out.UpdatedAt.IsZero() {
			t.Errorf("expected created_at / updated_at populated, got %+v", out)
		}

		got, err := c.Posts.GetByID(context.Background(), postA)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got == nil || got.Content != "hello world" {
			t.Fatalf("expected post restored, got %+v", got)
		}
	})

	t.Run("GetByID_missing_or_soft_deleted_returns_nil", func(t *testing.T) {
		c := newContract(t)
		got, err := c.Posts.GetByID(context.Background(), missingID)
		if err != nil {
			t.Fatalf("GetByID miss: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil on miss, got %+v", got)
		}

		seedUser(t, c.Users, userA)
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "x", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := c.Posts.Delete(context.Background(), postA); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		got, err = c.Posts.GetByID(context.Background(), postA)
		if err != nil {
			t.Fatalf("GetByID after delete: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil for soft-deleted, got %+v", got)
		}
	})

	t.Run("List_cursor_pagination_desc_by_id", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		// Three posts, IDs increase lexicographically (A < B < C).
		for _, id := range []string{postA, postB, postC} {
			if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: id, UserID: userA, Content: "c-" + id, Visibility: "public"}, nil, nil); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}

		page1, next1, err := c.Posts.List(context.Background(), nil, nil, 2)
		if err != nil {
			t.Fatalf("page1: %v", err)
		}
		if len(page1) != 2 {
			t.Fatalf("expected page1 size=2, got %d", len(page1))
		}
		// id DESC → C, B.
		if page1[0].ID != postC || page1[1].ID != postB {
			t.Fatalf("expected C,B got %s,%s", page1[0].ID, page1[1].ID)
		}
		if next1 == "" {
			t.Fatalf("expected non-empty cursor after first page")
		}

		page2, next2, err := c.Posts.List(context.Background(), nil, &next1, 2)
		if err != nil {
			t.Fatalf("page2: %v", err)
		}
		if len(page2) != 1 || page2[0].ID != postA {
			t.Fatalf("expected [A], got %+v", page2)
		}
		if next2 != "" {
			t.Fatalf("expected empty cursor after last page, got %q", next2)
		}
	})

	t.Run("List_filter_by_user_id_skips_replies", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedUser(t, c.Users, userB)

		// userA posts postA (top-level) + postC (reply to postA)
		// userB posts postB (top-level)
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "a", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed A: %v", err)
		}
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postB, UserID: userB, Content: "b", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed B: %v", err)
		}
		parentID := postA
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postC, UserID: userA, Content: "c", Visibility: "public", ParentPostID: &parentID}, nil, nil); err != nil {
			t.Fatalf("seed C: %v", err)
		}

		scoped := userA
		list, _, err := c.Posts.List(context.Background(), &scoped, nil, 10)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 1 || list[0].ID != postA {
			t.Fatalf("expected only postA, got %+v", list)
		}
	})

	t.Run("ListByUserIDs_unions_authors", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedUser(t, c.Users, userB)
		seedUser(t, c.Users, userC)

		for _, p := range []struct{ id, author string }{
			{postA, userA},
			{postB, userB},
			{postC, userC},
		} {
			if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: p.id, UserID: p.author, Content: p.id, Visibility: "public"}, nil, nil); err != nil {
				t.Fatalf("seed %s: %v", p.id, err)
			}
		}

		list, _, err := c.Posts.ListByUserIDs(context.Background(), []string{userA, userB}, nil, 10)
		if err != nil {
			t.Fatalf("ListByUserIDs: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 posts, got %d (%+v)", len(list), list)
		}
		// id DESC: postB then postA (C < B in our fixtures? C > B lexically)
		// Stable ULIDs: postA < postB < postC. DESC → C, B, A. Filter to A,B → B, A.
		if list[0].ID != postB || list[1].ID != postA {
			t.Fatalf("expected order [postB, postA], got [%s, %s]", list[0].ID, list[1].ID)
		}
	})

	t.Run("ListReplies_returns_only_replies_of_parent", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "parent", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed parent: %v", err)
		}
		parentID := postA
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postB, UserID: userA, Content: "r1", Visibility: "public", ParentPostID: &parentID}, nil, nil); err != nil {
			t.Fatalf("seed r1: %v", err)
		}
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postC, UserID: userA, Content: "top", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed top: %v", err)
		}

		replies, _, err := c.Posts.ListReplies(context.Background(), postA, nil, 10)
		if err != nil {
			t.Fatalf("ListReplies: %v", err)
		}
		if len(replies) != 1 || replies[0].ID != postB {
			t.Fatalf("expected [postB], got %+v", replies)
		}
	})

	t.Run("Counters_increment_and_floor_at_zero", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "x", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed: %v", err)
		}

		for i := 0; i < 3; i++ {
			if err := c.Posts.IncrementLikesCount(context.Background(), postA); err != nil {
				t.Fatalf("inc likes: %v", err)
			}
			if err := c.Posts.IncrementRepliesCount(context.Background(), postA); err != nil {
				t.Fatalf("inc replies: %v", err)
			}
		}
		for i := 0; i < 5; i++ {
			if err := c.Posts.DecrementLikesCount(context.Background(), postA); err != nil {
				t.Fatalf("dec likes: %v", err)
			}
		}

		p, err := c.Posts.GetByID(context.Background(), postA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if p.LikesCount != 0 {
			t.Errorf("expected likes floored at 0, got %d", p.LikesCount)
		}
		if p.RepliesCount != 3 {
			t.Errorf("expected replies=3, got %d", p.RepliesCount)
		}
	})

	// AttachOGP touches post_ogp which has an FK to ogp_cache on
	// postgres. Coverage lives alongside OGPCacheRepository contract
	// tests in Phase 3, where the pre-seeding harness exists; see
	// docs/tasks/08-wire-postgres-repository.md Phase 3-1.
}

// RunLikeRepositoryContract exercises LikeRepository. Requires Users
// and Posts for FK targets on postgres.
func RunLikeRepositoryContract(t *testing.T, newContract Factory) {
	t.Helper()

	seedPost := func(t *testing.T, c Contract, id, author string) {
		t.Helper()
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: id, UserID: author, Content: "x", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("seed post %s: %v", id, err)
		}
	}

	t.Run("Create_returns_true_on_insert_false_on_duplicate", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedPost(t, c, postA, userA)

		ok, err := c.Likes.Create(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if !ok {
			t.Fatalf("expected true on first insert")
		}
		ok, err = c.Likes.Create(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("create duplicate: %v", err)
		}
		if ok {
			t.Fatalf("expected false on duplicate insert")
		}
	})

	t.Run("Delete_returns_true_on_hit_false_on_miss", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedPost(t, c, postA, userA)

		if _, err := c.Likes.Create(context.Background(), userA, postA); err != nil {
			t.Fatalf("seed: %v", err)
		}
		ok, err := c.Likes.Delete(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("delete hit: %v", err)
		}
		if !ok {
			t.Fatalf("expected true on hit")
		}
		ok, err = c.Likes.Delete(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("delete miss: %v", err)
		}
		if ok {
			t.Fatalf("expected false on miss")
		}
	})

	t.Run("IsLikedBy_reflects_state", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedPost(t, c, postA, userA)

		is, err := c.Likes.IsLikedBy(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("is liked miss: %v", err)
		}
		if is {
			t.Fatalf("expected false before create")
		}
		if _, err := c.Likes.Create(context.Background(), userA, postA); err != nil {
			t.Fatalf("create: %v", err)
		}
		is, err = c.Likes.IsLikedBy(context.Background(), userA, postA)
		if err != nil {
			t.Fatalf("is liked hit: %v", err)
		}
		if !is {
			t.Fatalf("expected true after create")
		}
	})

	t.Run("ListLikedPostIDsByUser_filters_to_input_set", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)
		seedPost(t, c, postA, userA)
		seedPost(t, c, postB, userA)
		seedPost(t, c, postC, userA)

		if _, err := c.Likes.Create(context.Background(), userA, postA); err != nil {
			t.Fatalf("like A: %v", err)
		}
		if _, err := c.Likes.Create(context.Background(), userA, postC); err != nil {
			t.Fatalf("like C: %v", err)
		}

		got, err := c.Likes.ListLikedPostIDsByUser(context.Background(), userA, []string{postA, postB, postC})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 2 || !got[postA] || !got[postC] || got[postB] {
			t.Fatalf("unexpected result: %+v", got)
		}
	})
}
