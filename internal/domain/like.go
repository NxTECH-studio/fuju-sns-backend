package domain

import "time"

// Like represents the fact that UserID has liked PostID. Persistence is
// idempotent: repositories return a boolean so the app can adjust the
// denormalized likes counter at most once per state transition.
type Like struct {
	UserID    string    `json:"user_id"`
	PostID    string    `json:"post_id"`
	CreatedAt time.Time `json:"created_at"`
}
