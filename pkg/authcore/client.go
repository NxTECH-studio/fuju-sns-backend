// Package authcore contains the HTTP client used to talk to the AuthCore
// identity service and small helpers that let the SNS backend treat AuthCore
// as the source of truth for identity and session state.
package authcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Error sentinels distinguish "the session is definitely invalid" from
// "AuthCore is unreachable / returned a server error". Middleware uses this
// distinction: 401 for the former, 503 for the latter.
var (
	ErrInvalidSession = errors.New("authcore: invalid session")
	ErrUpstream       = errors.New("authcore: upstream error")
	ErrNotFound       = errors.New("authcore: resource not found")
)

// Session is the response of the introspection endpoint.
type Session struct {
	Sub       string    `json:"sub"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Profile is the response of the profile lookup endpoint.
type Profile struct {
	Sub         string `json:"sub"`
	DisplayName string `json:"display_name"`
	DisplayID   string `json:"display_id"`
	IconURL     string `json:"icon_url"`
}

// Client is the interface used by the rest of the codebase. Tests substitute
// a fake implementation.
type Client interface {
	Introspect(ctx context.Context, sessionToken string) (*Session, error)
	GetProfile(ctx context.Context, sub string) (*Profile, error)
}

// HTTPClient is an AuthCore Client backed by net/http.
type HTTPClient struct {
	baseURL        string
	serviceToken   string
	introspectPath string
	httpClient     *http.Client
	introspectTO   time.Duration
	getProfileTO   time.Duration
}

// Options configure an HTTPClient.
type Options struct {
	BaseURL           string
	ServiceToken      string
	IntrospectPath    string        // default: /internal/introspect
	IntrospectTimeout time.Duration // default: 500ms
	ProfileTimeout    time.Duration // default: 1s
	HTTPClient        *http.Client  // default: http.DefaultClient with 2s timeout
}

// New constructs an HTTPClient.
func New(opts Options) *HTTPClient {
	if opts.IntrospectPath == "" {
		opts.IntrospectPath = "/internal/introspect"
	}
	if opts.IntrospectTimeout == 0 {
		opts.IntrospectTimeout = 500 * time.Millisecond
	}
	if opts.ProfileTimeout == 0 {
		opts.ProfileTimeout = 1 * time.Second
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Second}
	}
	return &HTTPClient{
		baseURL:        strings.TrimRight(opts.BaseURL, "/"),
		serviceToken:   opts.ServiceToken,
		introspectPath: opts.IntrospectPath,
		httpClient:     httpClient,
		introspectTO:   opts.IntrospectTimeout,
		getProfileTO:   opts.ProfileTimeout,
	}
}

type introspectRequest struct {
	SessionToken string `json:"session_token"`
}

// Introspect validates the given opaque session token with AuthCore and
// returns the session it maps to. A 401 from AuthCore is returned as
// ErrInvalidSession; anything else (timeout, 5xx) as ErrUpstream.
func (c *HTTPClient) Introspect(ctx context.Context, sessionToken string) (*Session, error) {
	if sessionToken == "" {
		return nil, ErrInvalidSession
	}

	ctx, cancel := context.WithTimeout(ctx, c.introspectTO)
	defer cancel()

	body, err := json.Marshal(introspectRequest{SessionToken: sessionToken})
	if err != nil {
		return nil, fmt.Errorf("authcore: marshal introspect request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.introspectPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("authcore: build introspect request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, ErrInvalidSession
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status=%d", ErrUpstream, resp.StatusCode)
	}

	var out Session
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode body: %v", ErrUpstream, err)
	}
	if out.Sub == "" {
		return nil, fmt.Errorf("%w: empty sub in response", ErrUpstream)
	}
	return &out, nil
}

// GetProfile fetches the canonical profile for sub from AuthCore.
func (c *HTTPClient) GetProfile(ctx context.Context, sub string) (*Profile, error) {
	if sub == "" {
		return nil, fmt.Errorf("authcore: empty sub")
	}

	ctx, cancel := context.WithTimeout(ctx, c.getProfileTO)
	defer cancel()

	url := c.baseURL + "/internal/users/" + sub
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("authcore: build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status=%d", ErrUpstream, resp.StatusCode)
	}

	var out Profile
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode body: %v", ErrUpstream, err)
	}
	return &out, nil
}
