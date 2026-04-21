// Package tagextractor hosts concrete implementations of the
// domain.TagExtractor interface. RegexTagExtractor is the MVP: it pulls
// #hashtags out of post content and augments the result with a static
// keyword dictionary. A future JanomeTagExtractor / LLMTagExtractor can
// replace it without touching the use-case layer.
package tagextractor

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/fuju/backend/internal/domain"
)

// Hashtag pattern: `#` followed by a Unicode letter then up to 63 more
// letters / digits / underscores. `\p{L}` covers Kanji/Hiragana/Katakana
// as well as Latin letters.
var hashtagPattern = regexp.MustCompile(`#(\p{L}[\p{L}\p{N}_]{0,63})`)

// keywordEntry is a normalized dictionary keyword and its precompiled
// word-boundary pattern.
type keywordEntry struct {
	name    string
	pattern *regexp.Regexp
}

// RegexTagExtractor implements domain.TagExtractor by regex-matching
// hashtags and sweeping the content for a static keyword dictionary.
type RegexTagExtractor struct {
	// keywords is sorted by name so Extract is deterministic under the
	// MaxTagsPerPost cap (map iteration order would otherwise make the
	// winning set depend on Go's hash salt).
	keywords []keywordEntry
}

// NewRegexTagExtractor constructs a RegexTagExtractor with the given
// dictionary. `keywords` is normalized (lower-cased, trimmed) on the way
// in; the empty list is fine. Each keyword gets a precompiled pattern
// that requires a non-letter/digit boundary on either side so "fuji"
// does not match inside "fujitsu".
func NewRegexTagExtractor(keywords []string) *RegexTagExtractor {
	seen := make(map[string]struct{}, len(keywords))
	entries := make([]keywordEntry, 0, len(keywords))
	for _, kw := range keywords {
		norm := strings.ToLower(strings.TrimSpace(kw))
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		// `(^|[^\p{L}\p{N}_])<kw>($|[^\p{L}\p{N}_])` gives a unicode-aware
		// word boundary. The RE2 engine is linear, so this is ReDoS-safe.
		pat := regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])` + regexp.QuoteMeta(norm) + `($|[^\p{L}\p{N}_])`)
		entries = append(entries, keywordEntry{name: norm, pattern: pat})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return &RegexTagExtractor{keywords: entries}
}

// Extract returns normalized, deduplicated tag names with a hard cap of
// domain.MaxTagsPerPost. Hashtags come first (in appearance order), then
// dictionary hits in keyword-name order.
func (e *RegexTagExtractor) Extract(_ context.Context, content string) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, domain.MaxTagsPerPost)

	add := func(name string) {
		norm := strings.ToLower(strings.TrimSpace(name))
		if norm == "" {
			return
		}
		if len(norm) > domain.MaxTagNameLen {
			return
		}
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}

	for _, m := range hashtagPattern.FindAllStringSubmatch(content, -1) {
		if len(out) >= domain.MaxTagsPerPost {
			return out, nil
		}
		if len(m) >= 2 {
			add(m[1])
		}
	}

	if len(e.keywords) > 0 {
		for _, kw := range e.keywords {
			if len(out) >= domain.MaxTagsPerPost {
				break
			}
			if kw.pattern.MatchString(content) {
				add(kw.name)
			}
		}
	}
	return out, nil
}
