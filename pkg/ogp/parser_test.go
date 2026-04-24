package ogp

import (
	"strings"
	"testing"
)

func TestParse_OGPTakesPriority(t *testing.T) {
	body := `<html><head>
	  <meta property="og:title" content="OG Title">
	  <meta name="twitter:title" content="Twitter Title">
	  <title>HTML Title</title>
	  <meta property="og:description" content="OG desc">
	  <meta name="description" content="META desc">
	  <meta property="og:image" content="https://cdn.example.com/card.png">
	  <meta property="og:site_name" content="Example">
	  <meta property="og:url" content="https://example.com/canonical">
	</head></html>`
	m, err := Parse(strings.NewReader(body), "https://example.com/page")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Title != "OG Title" {
		t.Errorf("Title = %q, want OG Title", m.Title)
	}
	if m.Description != "OG desc" {
		t.Errorf("Description = %q, want OG desc", m.Description)
	}
	if m.ImageURL != "https://cdn.example.com/card.png" {
		t.Errorf("ImageURL = %q", m.ImageURL)
	}
	if m.SiteName != "Example" {
		t.Errorf("SiteName = %q", m.SiteName)
	}
	if m.CanonicalURL != "https://example.com/canonical" {
		t.Errorf("CanonicalURL = %q", m.CanonicalURL)
	}
}

func TestParse_FallsBackToTwitter(t *testing.T) {
	body := `<html><head>
	  <meta name="twitter:title" content="Twitter Title">
	  <meta name="twitter:description" content="Twitter desc">
	  <meta name="twitter:image" content="/img/t.png">
	</head></html>`
	m, _ := Parse(strings.NewReader(body), "https://example.com/")
	if m.Title != "Twitter Title" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.Description != "Twitter desc" {
		t.Errorf("Description = %q", m.Description)
	}
	if m.ImageURL != "https://example.com/img/t.png" {
		t.Errorf("relative image should resolve, got %q", m.ImageURL)
	}
}

func TestParse_FallsBackToTitle(t *testing.T) {
	body := `<html><head><title>Just HTML</title><meta name="description" content="Just meta"></head></html>`
	m, _ := Parse(strings.NewReader(body), "https://example.com/")
	if m.Title != "Just HTML" {
		t.Errorf("Title = %q", m.Title)
	}
	if m.Description != "Just meta" {
		t.Errorf("Description = %q", m.Description)
	}
}

func TestParse_CanonicalFromLink(t *testing.T) {
	body := `<html><head>
	  <link rel="canonical" href="/canonical">
	  <title>T</title>
	</head></html>`
	m, _ := Parse(strings.NewReader(body), "https://example.com/page?a=1")
	if m.CanonicalURL != "https://example.com/canonical" {
		t.Errorf("CanonicalURL = %q", m.CanonicalURL)
	}
}

func TestParse_MalformedHTMLDoesNotCrash(t *testing.T) {
	body := `<html><head><title>T</html>` // unterminated
	_, err := Parse(strings.NewReader(body), "https://example.com/")
	if err != nil {
		t.Errorf("Parse should be lenient on malformed HTML, got %v", err)
	}
}

func TestParse_TrimsTitleWhitespace(t *testing.T) {
	body := `<html><head><title>   Padded   </title></head></html>`
	m, _ := Parse(strings.NewReader(body), "https://example.com/")
	if m.Title != "Padded" {
		t.Errorf("expected trimmed title, got %q", m.Title)
	}
}

func TestParse_DropsUnsafeSchemesInURLs(t *testing.T) {
	// og:image pointing at javascript:, og:url pointing at data:. Both
	// must be dropped so they don't reach the cache.
	body := `<html><head>
	  <meta property="og:title" content="Evil">
	  <meta property="og:image" content="javascript:alert(1)">
	  <meta property="og:url" content="data:text/html,<script>alert(1)</script>">
	</head></html>`
	m, err := Parse(strings.NewReader(body), "https://example.com/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.ImageURL != "" {
		t.Errorf("ImageURL must be dropped, got %q", m.ImageURL)
	}
	if m.CanonicalURL != "" {
		t.Errorf("CanonicalURL must be dropped, got %q", m.CanonicalURL)
	}
}
