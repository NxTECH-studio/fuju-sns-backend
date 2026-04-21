package inmemory

import (
	"context"
	"testing"

	"github.com/fuju/backend/internal/domain"
)

func TestBadgeRepository_ListByUserIDs_Batches(t *testing.T) {
	ctx := context.Background()
	repo := NewBadgeRepository()

	dev, err := repo.Create(ctx, &domain.Badge{ID: "01BDEV0000000000000000000A", Key: "developer", Label: "dev", Priority: 5})
	if err != nil {
		t.Fatalf("seed dev: %v", err)
	}
	ver, err := repo.Create(ctx, &domain.Badge{ID: "01BVER0000000000000000000B", Key: "verified_celebrity", Label: "ver", Priority: 10})
	if err != nil {
		t.Fatalf("seed ver: %v", err)
	}

	if err := repo.Grant(ctx, "u1", dev.ID, "admin", nil, ""); err != nil {
		t.Fatalf("grant u1/dev: %v", err)
	}
	if err := repo.Grant(ctx, "u1", ver.ID, "admin", nil, ""); err != nil {
		t.Fatalf("grant u1/ver: %v", err)
	}
	if err := repo.Grant(ctx, "u2", ver.ID, "admin", nil, ""); err != nil {
		t.Fatalf("grant u2/ver: %v", err)
	}

	out, err := repo.ListByUserIDs(ctx, []string{"u1", "u2", "u3"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(out) != 2 {
		t.Errorf("expected 2 users with badges, got %d", len(out))
	}
	if len(out["u1"]) != 2 {
		t.Errorf("expected u1 to have 2 badges, got %d", len(out["u1"]))
	}
	// Priority-asc ordering: developer (5) before verified_celebrity (10).
	if out["u1"][0].Key != "developer" || out["u1"][1].Key != "verified_celebrity" {
		t.Errorf("expected priority-asc order, got %v/%v", out["u1"][0].Key, out["u1"][1].Key)
	}
	if len(out["u2"]) != 1 || out["u2"][0].Key != "verified_celebrity" {
		t.Errorf("expected u2 to have just verified_celebrity, got %v", out["u2"])
	}
	if _, ok := out["u3"]; ok {
		t.Errorf("expected u3 absent (no badges), got entry")
	}
}
