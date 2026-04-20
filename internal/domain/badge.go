// Package domain contains domain models and errors.
package domain

import (
	"time"
	"unicode/utf8"
)

// Badge is a master entry describing a profile decoration. The source of
// truth lives in this service's DB (badges table). MVP shipped seeds are
// "verified_celebrity" (blue) and "developer" (gold).
type Badge struct {
	ID          string // ULID (CHAR(26))
	Key         string // Machine-readable key, UNIQUE
	Label       string // Display name
	Description string
	IconURL     string
	Color       string // UI hint ("blue", "gold", …)
	Priority    int32  // Ascending display order
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UserBadge links a user (by AuthCore sub) to a badge with grant metadata.
type UserBadge struct {
	UserID    string // user sub (ULID)
	BadgeID   string // badge ID (ULID)
	GrantedAt time.Time
	GrantedBy string     // admin sub that granted the badge
	ExpiresAt *time.Time // nullable — past expirations are hidden at read time
	Reason    string     // admin memo (optional)
}

// GrantBadgeRequest is the admin API payload for POST
// /admin/users/{sub}/badges.
type GrantBadgeRequest struct {
	BadgeKey  string     `json:"badge_key"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

// Validate checks badge master fields are within DB bounds.
func (b *Badge) Validate() error {
	if b.Key == "" || len(b.Key) > 64 {
		return NewValidationError("key must be 1..64 chars")
	}
	if b.Label == "" || utf8.RuneCountInString(b.Label) > 64 {
		return NewValidationError("label must be 1..64 chars")
	}
	return nil
}
