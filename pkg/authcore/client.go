// Package authcore contains the HTTP client used to talk to the AuthCore
// identity service. AuthCore issues JWT Bearer access tokens; this backend
// validates each incoming request by asking AuthCore to introspect the
// presented token (RFC 7662). The profile endpoint then fills in the mirror
// cache stored in the users table.
package authcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Error sentinels distinguish "the session is definitely invalid" from
// "AuthCore is unreachable / returned a server error". Middleware maps the
// former to 401 and the latter to 503.
var (
	ErrInvalidSession = errors.New("authcore: invalid session")
	ErrUpstream       = errors.New("authcore: upstream error")
	ErrNotFound       = errors.New("authcore: resource not found")
)

// Session carries the fields we care about from an active-token
// introspection response. AuthCore returns the full RFC 7662 body; we keep
// only what we need.
type Session struct {
	Sub       string
	ExpiresAt time.Time
	PublicID  string // AuthCore's "username" field (= public_id / @handle)
}

// Profile mirrors the response of GET /v1/user/profile (AuthCore). Note the
// profile endpoint is keyed off the caller's own access token; there is no
// by-sub lookup.
type Profile struct {
	Sub      string // maps from AuthCore "id"
	PublicID string // maps from "public_id"
	IconURL  string // maps from "icon_url"
	Email    string // maps from "email"
}

// Client is the interface used by the rest of the codebase.
type Client interface {
	Introspect(ctx context.Context, accessToken string) (*Session, error)
	GetProfile(ctx context.Context, accessToken string) (*Profile, error)
}

// HTTPClient is an AuthCore Client backed by net/http.
type HTTPClient struct {
	baseURL        string
	clientID       string
	clientSecret   string
	introspectPath string
	profilePath    string
	httpClient     *http.Client
	introspectTO   time.Duration
	getProfileTO   time.Duration
}

// Options configure an HTTPClient.
type Options struct {
	BaseURL           string
	ClientID          string
	ClientSecret      string
	IntrospectPath    string        // default: /v1/auth/introspect
	ProfilePath       string        // default: /v1/user/profile
	IntrospectTimeout time.Duration // default: 500ms
	ProfileTimeout    time.Duration // default: 1s
	HTTPClient        *http.Client  // default: http.Client with 2s timeout
}

// New constructs an HTTPClient.
func New(opts Options) *HTTPClient {
	if opts.IntrospectPath == "" {
		opts.IntrospectPath = "/v1/auth/introspect"
	}
	if opts.ProfilePath == "" {
		opts.ProfilePath = "/v1/user/profile"
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
		clientID:       opts.ClientID,
		clientSecret:   opts.ClientSecret,
		introspectPath: opts.IntrospectPath,
		profilePath:    opts.ProfilePath,
		httpClient:     httpClient,
		introspectTO:   opts.IntrospectTimeout,
		getProfileTO:   opts.ProfileTimeout,
	}
}

// introspectResponse is the subset of RFC 7662 fields we parse. AuthCore
// returns additional fields (scope, client_id, mfa_verified, etc.) that we
// currently ignore.
type introspectResponse struct {
	Active   bool   `json:"active"`
	Sub      string `json:"sub,omitempty"`
	EXP      int64  `json:"exp,omitempty"`
	Username string `json:"username,omitempty"`
}

// Introspect validates an access token against AuthCore. `active: false`
// and 4xx client-auth errors surface as ErrInvalidSession; everything else
// (timeout, 5xx, malformed body) as ErrUpstream.
func (c *HTTPClient) Introspect(ctx context.Context, accessToken string) (*Session, error) {
	if accessToken == "" {
		return nil, ErrInvalidSession
	}

	ctx, cancel := context.WithTimeout(ctx, c.introspectTO)
	defer cancel()

	form := url.Values{}
	form.Set("token", accessToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.introspectPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("authcore: build introspect request: %w", err)
	}
	req.Header.Set("Authorization", basicAuth(c.clientID, c.clientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		// The backend itself failed client-auth — upstream config problem.
		return nil, fmt.Errorf("%w: client credentials rejected (status=%d)", ErrUpstream, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status=%d", ErrUpstream, resp.StatusCode)
	}

	var out introspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode body: %v", ErrUpstream, err)
	}
	if !out.Active {
		return nil, ErrInvalidSession
	}
	if out.Sub == "" {
		return nil, fmt.Errorf("%w: active response missing sub", ErrUpstream)
	}

	session := &Session{
		Sub:      out.Sub,
		PublicID: out.Username,
	}
	if out.EXP > 0 {
		session.ExpiresAt = time.Unix(out.EXP, 0)
	}
	return session, nil
}

// profileResponse is the subset of the AuthCore user profile response we
// consume.
type profileResponse struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	PublicID string  `json:"public_id"`
	IconURL  *string `json:"icon_url"`
}

// GetProfile fetches the caller's own profile from AuthCore using their
// access token. There is no by-sub lookup available, so the access token
// passed here should be the one issued for `sub`.
func (c *HTTPClient) GetProfile(ctx context.Context, accessToken string) (*Profile, error) {
	if accessToken == "" {
		return nil, ErrInvalidSession
	}

	ctx, cancel := context.WithTimeout(ctx, c.getProfileTO)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+c.profilePath, nil)
	if err != nil {
		return nil, fmt.Errorf("authcore: build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

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
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, ErrInvalidSession
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: unexpected status=%d", ErrUpstream, resp.StatusCode)
	}

	var out profileResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode body: %v", ErrUpstream, err)
	}

	profile := &Profile{
		Sub:      out.ID,
		PublicID: out.PublicID,
		Email:    out.Email,
	}
	if out.IconURL != nil {
		profile.IconURL = *out.IconURL
	}
	return profile, nil
}

// basicAuth builds the Basic Authorization header value.
func basicAuth(id, secret string) string {
	raw := id + ":" + secret
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}
