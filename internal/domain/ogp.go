package domain

import "time"

// OGP cache / job status constants. These are stored in VARCHAR columns
// and compared by string, so they're kept as exported consts rather than
// a typed enum to match the existing domain style (see badge status).
const (
	OGPStatusOK      = "ok"
	OGPStatusError   = "error"
	OGPStatusPending = "pending"

	OGPJobQueued  = "queued"
	OGPJobRunning = "running"
	OGPJobDone    = "done"
	OGPJobFailed  = "failed"
)

// OGPPreview is the cached Open Graph metadata for a normalized URL.
// URLHash is the SHA256 hex of the normalized URL and doubles as the
// cache primary key.
type OGPPreview struct {
	URLHash      string
	URL          string
	Title        string
	Description  string
	ImageURL     string
	SiteName     string
	CanonicalURL string
	FetchedAt    time.Time
	ExpiresAt    time.Time
	Status       string
	ErrorReason  string
}

// IsExpired reports whether the cached preview should be refetched.
func (p *OGPPreview) IsExpired(now time.Time) bool {
	return !now.Before(p.ExpiresAt)
}

// OGPJob is a single unit of work for the background fetch worker.
// Position is the 0-based index of the URL within the originating post's
// content; the worker uses it to attach the fetched preview at the same
// slot the enqueuer reserved, preserving order across the cache-hit and
// cache-miss branches.
type OGPJob struct {
	ID         string
	URLHash    string
	URL        string
	PostID     string
	Position   int
	EnqueuedAt time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	Status     string
	Attempts   int
	LastError  string
}
