package authcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ServiceTokenSource issues OAuth2 Client Credentials grants against
// AuthCore's POST /oauth/token. Used when this backend needs to call
// another service (fuju-emotion-model, etc.) on its own behalf rather
// than on behalf of an end user.
//
// Tokens are short-lived JWTs with `type=service`; AuthCore introspects
// them on the receiving side. Revocation = rotate the client secret.
type ServiceTokenSource interface {
	GetServiceToken(ctx context.Context, scope string) (string, error)
}

// IssueServiceToken implements the OAuth2 Client Credentials grant
// (RFC 6749 §4.4) against AuthCore. “scope“ may be empty, in which
// case AuthCore returns a token covering the client's full
// allowed_scope.
func (c *HTTPClient) IssueServiceToken(ctx context.Context, scope string) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, c.introspectTO)
	defer cancel()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if scope != "" {
		form.Set("scope", scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("authcore: build token request: %w", err)
	}
	req.Header.Set("Authorization", basicAuth(c.clientID, c.clientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 500:
		return "", 0, fmt.Errorf("%w: status=%d", ErrUpstream, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", 0, fmt.Errorf("%w: client credentials rejected (status=%d)", ErrUpstream, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", 0, fmt.Errorf("%w: unexpected status=%d", ErrUpstream, resp.StatusCode)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("%w: decode body: %v", ErrUpstream, err)
	}
	if out.AccessToken == "" {
		return "", 0, fmt.Errorf("%w: missing access_token", ErrUpstream)
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	return out.AccessToken, ttl, nil
}

// CachedServiceTokenSource wraps any token issuer with a single-token
// in-memory cache. Tokens are reused until “refreshLeadTime“ before
// expiry; a concurrent refresh is collapsed via mutex so a burst of
// callers shares one upstream call.
type CachedServiceTokenSource struct {
	issuer          serviceTokenIssuer
	refreshLeadTime time.Duration

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	scope     string
	now       func() time.Time
}

type serviceTokenIssuer interface {
	IssueServiceToken(ctx context.Context, scope string) (string, time.Duration, error)
}

// NewCachedServiceTokenSource wraps “issuer“ with a token cache.
// “refreshLeadTime“ (default 60s) controls how early a token is
// refreshed before its actual expiry; tune larger if your in-flight
// requests can run longer than 60s.
func NewCachedServiceTokenSource(issuer serviceTokenIssuer, refreshLeadTime time.Duration) *CachedServiceTokenSource {
	if refreshLeadTime <= 0 {
		refreshLeadTime = 60 * time.Second
	}
	return &CachedServiceTokenSource{
		issuer:          issuer,
		refreshLeadTime: refreshLeadTime,
		now:             time.Now,
	}
}

// GetServiceToken returns a cached service token if still fresh,
// otherwise issues a new one. “scope“ is part of the cache key:
// switching scope mid-session forces a refresh.
func (s *CachedServiceTokenSource) GetServiceToken(ctx context.Context, scope string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.token != "" && s.scope == scope && now.Add(s.refreshLeadTime).Before(s.expiresAt) {
		return s.token, nil
	}

	tok, ttl, err := s.issuer.IssueServiceToken(ctx, scope)
	if err != nil {
		return "", err
	}
	s.token = tok
	s.scope = scope
	s.expiresAt = now.Add(ttl)
	return tok, nil
}
