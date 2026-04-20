// Package domain contains domain models and errors.
package domain

import (
	"net/url"
	"time"
	"unicode/utf8"
)

// Badge field length caps — mirror the DB column bounds in migration 005.
const (
	maxBadgeKeyLen     = 64
	maxBadgeLabelRunes = 64
	maxBadgeIconURLLen = 1024
	maxBadgeColorLen   = 16
	maxGrantReasonLen  = 255
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

// Validate checks badge master fields are within DB bounds and that
// IconURL, if set, is an absolute http(s) URL — an admin-seeded javascript:
// URL would be an XSS sink on every profile that receives the badge.
func (b *Badge) Validate() error {
	if b.Key == "" || len(b.Key) > maxBadgeKeyLen {
		return NewValidationError("key must be 1..64 chars")
	}
	if b.Label == "" || utf8.RuneCountInString(b.Label) > maxBadgeLabelRunes {
		return NewValidationError("label must be 1..64 chars")
	}
	if len(b.IconURL) > maxBadgeIconURLLen {
		return NewValidationError("icon_url must be at most 1024 chars")
	}
	if b.IconURL != "" {
		u, err := url.Parse(b.IconURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return NewValidationError("icon_url must be an absolute http(s) URL")
		}
	}
	if len(b.Color) > maxBadgeColorLen {
		return NewValidationError("color must be at most 16 chars")
	}
	if b.Priority < 0 {
		return NewValidationError("priority must be non-negative")
	}
	return nil
}

// Validate checks grant request fields are within DB bounds.
func (r *GrantBadgeRequest) Validate() error {
	if r.BadgeKey == "" || len(r.BadgeKey) > maxBadgeKeyLen {
		return NewValidationError("badge_key must be 1..64 chars")
	}
	if len(r.Reason) > maxGrantReasonLen {
		return NewValidationError("reason must be at most 255 chars")
	}
	return nil
}
