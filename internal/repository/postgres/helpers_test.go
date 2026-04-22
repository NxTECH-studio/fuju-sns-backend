//go:build integration

package postgres_test

import (
	"time"

	"github.com/fuju/backend/internal/domain"
)

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func testUser(sub string) *domain.User {
	return &domain.User{
		Sub:                sub,
		DisplayNameCached:  sub,
		DisplayIDCached:    sub,
		ProfileRefreshedAt: time.Now().UTC(),
	}
}

func testPost(id, userID string) *domain.Post {
	return &domain.Post{
		ID:         id,
		UserID:     userID,
		Content:    "integration",
		Visibility: "public",
	}
}
