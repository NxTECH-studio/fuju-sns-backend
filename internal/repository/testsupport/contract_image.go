package testsupport

import (
	"context"
	"testing"

	"github.com/fuju/backend/internal/domain"
)

// Image fixtures. Only A / B / C are populated; the fourth slot of
// post_images (position 3) is exercised indirectly by the post
// creation contract.
const (
	imageA = "01HIMGAAAAAAAAAAAAAAAAAAAA"
	imageB = "01HIMGBBBBBBBBBBBBBBBBBBBB"
	imageC = "01HIMGCCCCCCCCCCCCCCCCCCCC"
)

// RunImageRepositoryContract exercises ImageRepository. On postgres,
// images has an FK to users(sub), so the Contract must include Users;
// the ListByPost* paths also require Posts.
func RunImageRepositoryContract(t *testing.T, newContract Factory) {
	t.Helper()

	makeImage := func(id, userID string) *domain.Image {
		return &domain.Image{
			ID:         id,
			StorageKey: "storage/" + id,
			FileName:   id + ".jpg",
			MimeType:   "image/jpeg",
			FileSize:   1024,
			PublicURL:  "https://cdn/" + id,
			UserID:     userID,
		}
	}

	t.Run("Create_then_GetByID", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		out, err := c.Images.Create(context.Background(), makeImage(imageA, userA))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if out == nil || out.ID != imageA {
			t.Fatalf("unexpected: %+v", out)
		}
		if out.CreatedAt.IsZero() {
			t.Errorf("expected created_at populated")
		}

		got, err := c.Images.GetByID(context.Background(), imageA)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got == nil || got.StorageKey != "storage/"+imageA {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("GetByID_missing_or_soft_deleted_returns_nil", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		got, err := c.Images.GetByID(context.Background(), missingID)
		if err != nil {
			t.Fatalf("get miss: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil: %+v", got)
		}

		if _, err := c.Images.Create(context.Background(), makeImage(imageA, userA)); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := c.Images.Delete(context.Background(), imageA); err != nil {
			t.Fatalf("delete: %v", err)
		}
		got, err = c.Images.GetByID(context.Background(), imageA)
		if err != nil {
			t.Fatalf("get deleted: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil on soft-deleted, got %+v", got)
		}
	})

	t.Run("GetByUserID_filters_soft_deleted", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		for _, id := range []string{imageA, imageB, imageC} {
			if _, err := c.Images.Create(context.Background(), makeImage(id, userA)); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}
		if err := c.Images.Delete(context.Background(), imageB); err != nil {
			t.Fatalf("delete: %v", err)
		}

		got, err := c.Images.GetByUserID(context.Background(), userA)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d", len(got))
		}
		for _, img := range got {
			if img.ID == imageB {
				t.Errorf("soft-deleted image leaked: %+v", img)
			}
		}
	})

	t.Run("ListByPostID_ordered_by_position", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		// Seed images A, B, C and attach to postA in that order —
		// PostRepository.Create's imageIDs ordering becomes
		// post_images.position (0..N-1).
		for _, id := range []string{imageA, imageB, imageC} {
			if _, err := c.Images.Create(context.Background(), makeImage(id, userA)); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}
		if _, err := c.Posts.Create(context.Background(), &domain.Post{
			ID: postA, UserID: userA, Content: "x", Visibility: "public",
		}, []string{imageA, imageB, imageC}, nil); err != nil {
			t.Fatalf("post: %v", err)
		}

		got, err := c.Images.ListByPostID(context.Background(), postA)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 3 || got[0].ID != imageA || got[1].ID != imageB || got[2].ID != imageC {
			t.Fatalf("expected [A,B,C] in position order, got %+v", got)
		}
	})

	t.Run("ListByPostIDs_groups_and_drops_bare_posts", func(t *testing.T) {
		c := newContract(t)
		seedUser(t, c.Users, userA)

		for _, id := range []string{imageA, imageB} {
			if _, err := c.Images.Create(context.Background(), makeImage(id, userA)); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postA, UserID: userA, Content: "a", Visibility: "public"}, []string{imageA, imageB}, nil); err != nil {
			t.Fatalf("post A: %v", err)
		}
		// postB has no images.
		if _, err := c.Posts.Create(context.Background(), &domain.Post{ID: postB, UserID: userA, Content: "b", Visibility: "public"}, nil, nil); err != nil {
			t.Fatalf("post B: %v", err)
		}

		got, err := c.Images.ListByPostIDs(context.Background(), []string{postA, postB})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 entry (postB absent), got %d (%+v)", len(got), got)
		}
		if len(got[postA]) != 2 || got[postA][0].ID != imageA {
			t.Errorf("postA wrong: %+v", got[postA])
		}
	})
}
