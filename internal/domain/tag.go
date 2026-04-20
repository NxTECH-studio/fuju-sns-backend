package domain

import (
	"context"
	"time"
)

// MaxTagsPerPost caps how many tags a single post may carry. Matches the
// TagExtractor truncation policy; keeps the post_tags fanout predictable.
const MaxTagsPerPost = 10

// MaxTagNameLen is the rune-count upper bound on Tag.Name, mirroring the
// VARCHAR(64) column in migration 004.
const MaxTagNameLen = 64

// Tag is a normalized, deduplicated label derived from a post's content
// (or, in the future, from user input). Names are stored lower-cased and
// whitespace-trimmed so UNIQUE(name) is meaningful.
type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// PostTag is the join row between Post and Tag. No application logic uses
// the standalone row today; it exists so a SQL repository can return it.
type PostTag struct {
	PostID string
	TagID  string
}

// TagExtractor produces tag candidates from raw post content. The interface
// is intentionally small so the MVP Regex implementation can later be
// swapped for a morphological (Janome) or LLM-backed one without touching
// the use-case layer.
//
// Extract must return normalized (lower-cased, trimmed), de-duplicated
// names. Callers are allowed to further truncate to MaxTagsPerPost but
// implementations that can enforce it cheaply should do so.
type TagExtractor interface {
	Extract(ctx context.Context, content string) ([]string, error)
}
