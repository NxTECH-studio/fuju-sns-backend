package ogp

import (
	"regexp"
	"strings"
)

// MaxExtractedURLs caps how many URLs the extractor returns per post.
// Excess URLs are silently dropped so a pathological post can't spam
// the job queue. The MVP UI renders only the first anyway.
const MaxExtractedURLs = 5

// urlRE matches bare http(s) URLs in free text. It stops before common
// trailing punctuation so sentence-ending chars aren't glued onto the
// URL.
//
// Not RFC-perfect — the goal is "extract well-formed URLs from user
// prose without false positives". The normalizer is the authoritative
// validator downstream.
var urlRE = regexp.MustCompile("https?://[^\\s<>\"'`]+")

// trailingTrim lists characters that are commonly adjacent to a URL in
// prose but are not part of the URL itself. We strip them from the
// right edge after the regex match. Balancing pairs ())( ])[ }{) are
// handled separately below.
const trailingTrim = ".,;:!?\"'\\*>}]"

// ExtractURLs returns up to MaxExtractedURLs distinct URLs from content
// in the order they first appear. The returned strings are raw (not yet
// normalized) — callers pass each through Normalize before hashing.
func ExtractURLs(content string) []string {
	matches := urlRE.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		m = trimTrailing(m)
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
		if len(out) >= MaxExtractedURLs {
			break
		}
	}
	return out
}

// trimTrailing strips trailing punctuation that is almost certainly not
// part of the URL. It also drops a lone unbalanced `)` `]` `}` when the
// URL contains no matching opener — this handles patterns like
// "see (https://example.com/path)" where the closing paren is prose.
func trimTrailing(s string) string {
	for len(s) > 0 {
		last := s[len(s)-1]
		if strings.IndexByte(trailingTrim, last) >= 0 {
			s = s[:len(s)-1]
			continue
		}
		if last == ')' && strings.Count(s, "(") < strings.Count(s, ")") {
			s = s[:len(s)-1]
			continue
		}
		if last == ']' && strings.Count(s, "[") < strings.Count(s, "]") {
			s = s[:len(s)-1]
			continue
		}
		if last == '}' && strings.Count(s, "{") < strings.Count(s, "}") {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
