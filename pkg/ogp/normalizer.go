// Package ogp implements URL normalization, extraction, SSRF-safe HTTP
// fetch, and HTML metadata parsing for the Open Graph Protocol preview
// feature.
package ogp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

// trackingParams lists query keys stripped during normalization. Keeping
// them out of the cache key lifts the hit rate because the same article
// on the same domain is often shared with different campaign tracking.
var trackingParams = map[string]struct{}{
	"utm_source":   {},
	"utm_medium":   {},
	"utm_campaign": {},
	"utm_term":     {},
	"utm_content":  {},
	"gclid":        {},
	"fbclid":       {},
	"ref":          {},
	"mc_cid":       {},
	"mc_eid":       {},
}

// Scheme constants used across the package's scheme allow-list checks.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// defaultPorts drops the URL port segment when the scheme's default
// matches.
var defaultPorts = map[string]string{
	schemeHTTP:  "80",
	schemeHTTPS: "443",
}

// multiSlashRE collapses "//" and longer runs to a single "/". Applied
// after the leading "scheme://" is stripped, so the path-only match is
// safe.
var multiSlashRE = regexp.MustCompile(`/+`)

// Normalize applies the canonicalization rules documented in the task
// spec: lowercase scheme/host, drop default ports, collapse slashes,
// strip the trailing slash (except for root "/"), drop fragment, sort
// query keys, and remove known tracking parameters. Non-ASCII hosts are
// punycode-encoded.
//
// Only http and https schemes are supported; other inputs return an
// error.
func Normalize(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty URL")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != schemeHTTP && scheme != schemeHTTPS {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Scheme = scheme

	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", fmt.Errorf("missing host")
	}
	// IP literals (IPv4 or IPv6) skip IDN processing — idna rejects the
	// colons in IPv6 literals and there's nothing to punycode anyway.
	if net.ParseIP(host) == nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			return "", fmt.Errorf("idna: %w", err)
		}
		host = ascii
	}

	port := u.Port()
	if port != "" && defaultPorts[scheme] == port {
		port = ""
	}
	// IPv6 literals must stay bracketed in the host:port form. The
	// bracket-stripped form ("::1:8080") parses as a different URL on
	// round-trip, which would break the cache key.
	u.Host = joinHostPort(host, port)

	u.Path = normalizePath(u.Path)
	u.Fragment = ""
	u.RawFragment = ""
	u.RawQuery = normalizeQuery(u.RawQuery)

	return u.String(), nil
}

// normalizePath collapses repeated slashes and strips the trailing slash
// except for the root "/". An empty input becomes "/" so two variants of
// the same host ("https://x" and "https://x/") collapse to one.
func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	cleaned := multiSlashRE.ReplaceAllString(path, "/")
	if cleaned == "/" {
		return "/"
	}
	return strings.TrimRight(cleaned, "/")
}

// normalizeQuery filters out known tracking keys and sorts the remaining
// keys alphabetically. Values keep their original percent-encoding (we
// parse and re-encode via url.Values which preserves canonical form).
func normalizeQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		// Fallback: opaque, return as-is. Don't fail the whole URL on
		// unparseable query — best-effort normalization.
		return raw
	}
	for k := range values {
		if _, drop := trackingParams[k]; drop {
			delete(values, k)
		}
	}
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		// Keep each key's values in the original submission order; only
		// the keys are sorted. This matches common implementations and
		// preserves (e.g.) ordered pagination params.
		for j, v := range values[k] {
			if i > 0 || j > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v))
		}
	}
	return b.String()
}

// Hash returns the SHA256 hex of a normalized URL. It is a pure function
// on the already-normalized string — callers must normalize first.
func Hash(normalizedURL string) string {
	sum := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(sum[:])
}

// joinHostPort rebuilds the URL host segment after the bracket / port
// have been split. IPv6 literals (detected by a colon in the host)
// must stay bracketed to remain round-trippable; net.JoinHostPort does
// the right thing for that case and works for IPv4 / hostnames too.
func joinHostPort(host, port string) string {
	if port == "" {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}
