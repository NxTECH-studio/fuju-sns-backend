// Package domain contains domain models and errors.
package domain

import (
	"time"
)

// User is the SNS-local mirror of an AuthCore identity plus SNS-owned profile
// attributes. The source of truth for Sub / DisplayName / DisplayID / IconURL
// is AuthCore; the *_Cached fields are a 1h-TTL cache. Bio, BannerURL and
// IsAdmin are owned by this service.
type User struct {
	Sub                string // AuthCore sub (ULID, 26 chars)
	DisplayNameCached  string
	DisplayIDCached    string
	IconURLCached      string
	ProfileRefreshedAt time.Time

	Bio       string
	BannerURL string

	IsAdmin bool

	// Denormalized counters maintained by the follow usecases. SQL stores
	// them on the users row; a background reconciliation job (future work)
	// would reset drift.
	FollowersCount int64
	FollowingCount int64

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// UpdateUserProfileRequest carries the subset of fields a user may modify on
// their own profile. IsAdmin is intentionally omitted — it is not
// self-mutable.
type UpdateUserProfileRequest struct {
	Bio       *string
	BannerURL *string
}

// Validate checks the SNS-owned fields for bounds. AuthCore-owned fields are
// validated by AuthCore and not re-checked here.
func (u *User) Validate() error {
	if len(u.Bio) > 500 {
		return &InvalidUserError{Reason: "bio must be 500 characters or fewer"}
	}
	if len(u.BannerURL) > 1024 {
		return &InvalidUserError{Reason: "banner url must be 1024 characters or fewer"}
	}
	return nil
}
