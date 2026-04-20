// Package tagextractor hosts concrete implementations of the
// domain.TagExtractor interface. RegexTagExtractor is the MVP: it pulls
// #hashtags out of post content and augments the result with a static
// keyword dictionary. A future JanomeTagExtractor / LLMTagExtractor can
// replace it without touching the use-case layer.
package tagextractor

import (
	"context"
	"regexp"
	"strings"

	"github.com/fuju/backend/internal/domain"
)

// Hashtag pattern: `#` followed by a Unicode letter then up to 63 more
// letters / digits / underscores. `\p{L}` covers Kanji/Hiragana/Katakana
// as well as Latin letters.
var hashtagPattern = regexp.MustCompile(`#(\p{L}[\p{L}\p{N}_]{0,63})`)

// RegexTagExtractor implements domain.TagExtractor by regex-matching
// hashtags and sweeping the content for a static keyword dictionary.
type RegexTagExtractor struct {
	// keywords is a set of normalized (lower-case) keywords to flag as tags
	// whenever they appear as a whole word in the content. Nil or empty
	// disables the dictionary sweep.
	keywords map[string]struct{}
}

// NewRegexTagExtractor constructs a RegexTagExtractor with the given
// dictionary. `keywords` is normalized (lower-cased, trimmed) on the way
// in; the empty list is fine.
func NewRegexTagExtractor(keywords []string) *RegexTagExtractor {
	set := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		norm := strings.ToLower(strings.TrimSpace(kw))
		if norm == "" {
			continue
		}
		set[norm] = struct{}{}
	}
	return &RegexTagExtractor{keywords: set}
}

// Extract returns normalized, deduplicated tag names with a hard cap of
// domain.MaxTagsPerPost. Hashtags come first (in appearance order), then
// dictionary hits.
func (e *RegexTagExtractor) Extract(_ context.Context, content string) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, domain.MaxTagsPerPost)

	add := func(name string) bool {
		norm := strings.ToLower(strings.TrimSpace(name))
		if norm == "" {
			return false
		}
		if len(norm) > domain.MaxTagNameLen {
			return false
		}
		if _, dup := seen[norm]; dup {
			return false
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
		return true
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
		lower := strings.ToLower(content)
		for kw := range e.keywords {
			if len(out) >= domain.MaxTagsPerPost {
				break
			}
			if strings.Contains(lower, kw) {
				add(kw)
			}
		}
	}
	return out, nil
}
