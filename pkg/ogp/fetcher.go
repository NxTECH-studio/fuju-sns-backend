package ogp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fuju/backend/internal/domain"
)

// DefaultUserAgent is used when Options.UserAgent is empty. Identifies
// the bot so sites can opt out via robots.txt or rate-limit us.
const DefaultUserAgent = "FujuBot/1.0 (+https://fuju.example.com/bot)"

// DefaultAcceptLanguage biases site negotiation toward English + Japanese
// metadata, matching the product's primary audience.
const DefaultAcceptLanguage = "en,ja;q=0.8"

// MaxBodyBytes caps the HTML body read. Most real-world pages are well
// under 1 MiB; 5 MiB is a generous ceiling that blocks a malicious site
// from streaming gigabytes at us.
const MaxBodyBytes = int64(5 * 1024 * 1024)

// ErrNotHTML is returned when the server responds with a non-HTML
// content type. Non-retriable.
var ErrNotHTML = errors.New("ogp: response is not HTML")

// ErrBadStatus is returned on any 4xx / 5xx status code. Retriability
// is derived from the status: 5xx is retriable, 4xx is not.
type ErrBadStatus struct{ Status int }

func (e *ErrBadStatus) Error() string { return fmt.Sprintf("ogp: bad status %d", e.Status) }

// Retriable reports whether the HTTP error is worth retrying. Used by
// the worker to decide whether to requeue vs. record a long-lived
// error cache row.
func Retriable(err error) bool {
	var bs *ErrBadStatus
	if errors.As(err, &bs) {
		return bs.Status >= 500
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Network errors are retriable; SSRF / scheme errors are not.
	if errors.Is(err, ErrBlockedAddress) || errors.Is(err, ErrBlockedScheme) {
		return false
	}
	if errors.Is(err, ErrNotHTML) {
		return false
	}
	return true
}

// Options configures a Fetcher. Zero values are replaced with defaults.
type Options struct {
	UserAgent      string
	AcceptLanguage string
	Timeout        time.Duration
	Client         *http.Client
}

// Fetcher is the coordinator: it normalizes the URL, issues a
// SSRF-guarded GET, and parses the HTML for OGP metadata.
type Fetcher struct {
	client         *http.Client
	userAgent      string
	acceptLanguage string
	now            func() time.Time
}

// NewFetcher builds a Fetcher. A nil opts is equivalent to Options{}.
func NewFetcher(opts *Options) *Fetcher {
	if opts == nil {
		opts = &Options{}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	lang := opts.AcceptLanguage
	if lang == "" {
		lang = DefaultAcceptLanguage
	}
	client := opts.Client
	if client == nil {
		client = NewSafeClient(opts.Timeout)
	}
	return &Fetcher{
		client:         client,
		userAgent:      ua,
		acceptLanguage: lang,
		now:            time.Now,
	}
}

// Fetch retrieves and parses a URL. The returned OGPPreview has
// FetchedAt set; ExpiresAt / Status are left to the caller (worker)
// because TTL depends on success vs. failure policy.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*domain.OGPPreview, error) {
	normalized, err := Normalize(rawURL)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	req.Header.Set("Accept-Language", f.acceptLanguage)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &ErrBadStatus{Status: resp.StatusCode}
	}
	ct := resp.Header.Get("Content-Type")
	if !isHTMLContentType(ct) {
		return nil, ErrNotHTML
	}

	// Cap the body. http.MaxBytesReader errors on overflow with a
	// specific message; we wrap it so callers see a stable marker.
	body := http.MaxBytesReader(nil, resp.Body, MaxBodyBytes)
	// Use the final URL (resp.Request.URL) as the base so redirect-
	// targeted relative og:image paths resolve correctly.
	baseURL := normalized
	if resp.Request != nil && resp.Request.URL != nil {
		baseURL = resp.Request.URL.String()
	}
	meta, err := Parse(body, baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	now := f.now()
	return &domain.OGPPreview{
		URLHash:      Hash(normalized),
		URL:          normalized,
		Title:        meta.Title,
		Description:  meta.Description,
		ImageURL:     meta.ImageURL,
		SiteName:     meta.SiteName,
		CanonicalURL: meta.CanonicalURL,
		FetchedAt:    now,
	}, nil
}

// isHTMLContentType accepts text/html and application/xhtml+xml, with
// or without charset parameters. Case-insensitive.
func isHTMLContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		// Empty content-type is ambiguous; allow and let parser decide.
		return true
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct == "text/html" || ct == "application/xhtml+xml"
}
