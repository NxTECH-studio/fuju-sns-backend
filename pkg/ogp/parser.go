package ogp

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Metadata is the parser's output: everything the OGP cache row needs
// minus fetch bookkeeping (fetched_at, expires_at, status).
type Metadata struct {
	Title        string
	Description  string
	ImageURL     string
	SiteName     string
	CanonicalURL string
}

// Parse walks the HTML document at r and returns the best OGP metadata
// available. Priority per field:
//
//  1. <meta property="og:*">
//  2. <meta name="twitter:*">
//  3. <title> / <meta name="description">
//
// The ImageURL is resolved against baseURL so a relative og:image like
// "/img/card.png" becomes absolute. baseURL should be the final URL
// after any redirects.
//
// Malformed HTML does not error — the parser returns whatever it could
// extract and leaves the rest empty.
func Parse(r io.Reader, baseURL string) (*Metadata, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	m := &Metadata{}
	var title string
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "title":
			if title == "" {
				title = textOf(n)
			}
		case "meta":
			applyMeta(n, m)
		case "link":
			applyLink(n, m)
		}
	})

	// Fallback chain for individual fields. og:* → twitter:* → <title>.
	if m.Title == "" {
		m.Title = title
	}
	// Resolve relative og:image / og:url to absolute using baseURL, and
	// drop anything that doesn't resolve to http(s). This is the last
	// line of defense against sites serving `javascript:` or `data:`
	// URIs in og:image / og:url — the safe-HTTP client already enforces
	// http(s) on fetch, but cached metadata flows straight to the API
	// response.
	m.ImageURL = resolveSafeURL(baseURL, m.ImageURL)
	m.CanonicalURL = resolveSafeURL(baseURL, m.CanonicalURL)
	return m, nil
}

// walk is a depth-first visitor over every node under root.
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

// applyMeta inspects a <meta> element and writes into m when the tag's
// property/name matches a field we care about. og:* always wins over
// twitter:*; a non-empty og:title never gets overwritten by a later
// twitter:title.
func applyMeta(n *html.Node, m *Metadata) {
	var property, name, content string
	for _, a := range n.Attr {
		switch a.Key {
		case "property":
			property = a.Val
		case "name":
			name = a.Val
		case "content":
			content = a.Val
		}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	switch property {
	case "og:title":
		m.Title = content
	case "og:description":
		m.Description = content
	case "og:image", "og:image:secure_url", "og:image:url":
		if m.ImageURL == "" {
			m.ImageURL = content
		}
	case "og:site_name":
		m.SiteName = content
	case "og:url":
		m.CanonicalURL = content
	}
	// twitter:* fallbacks. Only apply when the corresponding og:* field
	// is still empty, so priority is preserved.
	switch name {
	case "twitter:title":
		if m.Title == "" {
			m.Title = content
		}
	case "twitter:description":
		if m.Description == "" {
			m.Description = content
		}
	case "twitter:image", "twitter:image:src":
		if m.ImageURL == "" {
			m.ImageURL = content
		}
	case "description":
		if m.Description == "" {
			m.Description = content
		}
	}
}

// applyLink picks up <link rel="canonical" href="..."> as a final
// fallback for CanonicalURL when og:url is absent.
func applyLink(n *html.Node, m *Metadata) {
	if m.CanonicalURL != "" {
		return
	}
	var rel, href string
	for _, a := range n.Attr {
		switch a.Key {
		case "rel":
			rel = a.Val
		case "href":
			href = a.Val
		}
	}
	if rel == "canonical" {
		m.CanonicalURL = strings.TrimSpace(href)
	}
}

// textOf returns the concatenated text of a node's direct-text
// descendants. For <title> this is the title string. Trailing / leading
// whitespace is trimmed because many sites pad their titles.
func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return strings.TrimSpace(b.String())
}

// resolveURL joins a possibly-relative ref against a base URL. Returns
// "" if either fails to parse.
func resolveURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil || !b.IsAbs() {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

// resolveSafeURL is resolveURL plus a scheme allow-list: the final
// absolute URL must be http or https. This prevents a malicious site
// from smuggling `javascript:`, `data:`, or `file:` URIs into
// og:image / og:url, which would otherwise be cached and served back
// to clients. Returns "" when the ref is empty, unresolvable, or
// resolves to a disallowed scheme.
func resolveSafeURL(base, ref string) string {
	abs := resolveURL(base, ref)
	if abs == "" {
		return ""
	}
	u, err := url.Parse(abs)
	if err != nil {
		return ""
	}
	if u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS {
		return ""
	}
	return abs
}
