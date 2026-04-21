package ogp

import (
	"fmt"
	"reflect"
	"testing"
)

func TestExtractURLs_Basic(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "no urls here", nil},
		{"single http", "check https://example.com/path please", []string{"https://example.com/path"}},
		{"single http (not https)", "go http://example.com/x", []string{"http://example.com/x"}},
		{"scheme-less is skipped", "example.com is a site", nil},
		{"strips trailing period", "visit https://example.com/path.", []string{"https://example.com/path"}},
		{"strips trailing comma", "https://example.com/a, and more", []string{"https://example.com/a"}},
		{"strips trailing paren when unbalanced", "see (https://example.com/x)", []string{"https://example.com/x"}},
		{"keeps paren when balanced", "https://en.wikipedia.org/wiki/Go_(programming_language)", []string{"https://en.wikipedia.org/wiki/Go_(programming_language)"}},
		{"dedup", "https://a.example/ and https://a.example/", []string{"https://a.example/"}},
		{"multiple distinct", "a https://x.example/ b https://y.example/", []string{"https://x.example/", "https://y.example/"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractURLs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExtractURLs(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractURLs_CapAtMax(t *testing.T) {
	var b []byte
	for i := 0; i < MaxExtractedURLs+3; i++ {
		b = append(b, fmt.Sprintf(" https://example.com/p/%d", i)...)
	}
	got := ExtractURLs(string(b))
	if len(got) != MaxExtractedURLs {
		t.Errorf("expected %d URLs (cap), got %d", MaxExtractedURLs, len(got))
	}
}

func TestExtractURLs_StripsQuoteBacktick(t *testing.T) {
	got := ExtractURLs("hello `https://example.com/path` world")
	if len(got) != 1 || got[0] != "https://example.com/path" {
		t.Errorf("backtick should not be part of URL; got %v", got)
	}
}
