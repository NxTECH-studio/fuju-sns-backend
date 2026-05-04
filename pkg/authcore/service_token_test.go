package authcore

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPClient_IssueServiceToken(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantErr   error
		wantToken string
		wantTTL   time.Duration
	}{
		{
			name:      "success",
			status:    http.StatusOK,
			body:      `{"access_token":"svc_xxx","token_type":"Bearer","expires_in":3600}`,
			wantToken: "svc_xxx", wantTTL: 1 * time.Hour,
		},
		{
			name:    "missing_token",
			status:  http.StatusOK,
			body:    `{"token_type":"Bearer","expires_in":3600}`,
			wantErr: ErrUpstream,
		},
		{
			name: "5xx", status: http.StatusBadGateway, body: ``, wantErr: ErrUpstream,
		},
		{
			name:   "client_credentials_rejected",
			status: http.StatusUnauthorized, body: ``, wantErr: ErrUpstream,
		},
		{
			name:   "unexpected_status",
			status: http.StatusTeapot, body: ``, wantErr: ErrUpstream,
		},
		{
			name:      "expires_in_missing_falls_back_1h",
			status:    http.StatusOK,
			body:      `{"access_token":"svc_xxx","token_type":"Bearer"}`,
			wantToken: "svc_xxx", wantTTL: time.Hour,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oauth/token" {
					t.Fatalf("path=%q", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Fatalf("method=%q", r.Method)
				}
				// Basic Auth header is sanity-checked.
				authHdr := r.Header.Get("Authorization")
				if !strings.HasPrefix(authHdr, "Basic ") {
					t.Fatalf("Authorization=%q (want Basic ...)", authHdr)
				}
				raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHdr, "Basic "))
				if string(raw) != "fuju-sns:s3cret" {
					t.Fatalf("creds=%q", string(raw))
				}
				if err := r.ParseForm(); err != nil {
					t.Fatalf("ParseForm: %v", err)
				}
				if r.PostFormValue("grant_type") != "client_credentials" {
					t.Fatalf("grant_type=%q", r.PostFormValue("grant_type"))
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			t.Cleanup(srv.Close)

			c := New(Options{BaseURL: srv.URL, ClientID: "fuju-sns", ClientSecret: "s3cret"})
			tok, ttl, err := c.IssueServiceToken(context.Background(), "")

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v want=%v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tok != tc.wantToken {
				t.Fatalf("token=%q want=%q", tok, tc.wantToken)
			}
			if ttl != tc.wantTTL {
				t.Fatalf("ttl=%v want=%v", ttl, tc.wantTTL)
			}
		})
	}
}

func TestHTTPClient_IssueServiceToken_ScopeForwarded(t *testing.T) {
	var seenScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seenScope = r.PostFormValue("scope")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":60}`))
	}))
	t.Cleanup(srv.Close)

	c := New(Options{BaseURL: srv.URL, ClientID: "fuju-sns", ClientSecret: "s3cret"})
	_, _, err := c.IssueServiceToken(context.Background(), "ingest:events")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if seenScope != "ingest:events" {
		t.Fatalf("scope=%q want=ingest:events", seenScope)
	}
}

// rotatedTok is the token value returned by fakeIssuer after a
// rotation in cache tests; deduped to keep goconst quiet.
const rotatedTok = "tok2"

// fakeIssuer implements serviceTokenIssuer for cache tests.
type fakeIssuer struct {
	mu    sync.Mutex
	calls int
	tok   string
	ttl   time.Duration
	err   error
}

func (f *fakeIssuer) IssueServiceToken(_ context.Context, _ string) (string, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return "", 0, f.err
	}
	return f.tok, f.ttl, nil
}

func (f *fakeIssuer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestCachedServiceTokenSource_HitOnSecondCall(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	iss := &fakeIssuer{tok: "tok1", ttl: 5 * time.Minute}
	c := NewCachedServiceTokenSource(iss, 60*time.Second)
	c.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		tok, err := c.GetServiceToken(context.Background(), "scope-a")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if tok != "tok1" {
			t.Fatalf("call %d tok=%q", i, tok)
		}
	}
	if got := iss.callCount(); got != 1 {
		t.Fatalf("issuer calls=%d want=1", got)
	}
}

func TestCachedServiceTokenSource_RefreshNearExpiry(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	iss := &fakeIssuer{tok: "tok1", ttl: 90 * time.Second}
	c := NewCachedServiceTokenSource(iss, 60*time.Second)
	c.now = func() time.Time { return now }

	// First call: cache miss.
	if _, err := c.GetServiceToken(context.Background(), "s"); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Advance to within refresh-lead-time of expiry → must refresh.
	now = now.Add(45 * time.Second) // 45s in, expires at 90s, leadTime 60s → refresh
	iss.tok = rotatedTok
	tok, err := c.GetServiceToken(context.Background(), "s")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if tok != rotatedTok {
		t.Fatalf("tok=%q want=tok2", tok)
	}
	if got := iss.callCount(); got != 2 {
		t.Fatalf("issuer calls=%d want=2", got)
	}
}

func TestCachedServiceTokenSource_DifferentScopeMisses(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	iss := &fakeIssuer{tok: "tok1", ttl: time.Hour}
	c := NewCachedServiceTokenSource(iss, 60*time.Second)
	c.now = func() time.Time { return now }

	if _, err := c.GetServiceToken(context.Background(), "scope-a"); err != nil {
		t.Fatalf("scope-a: %v", err)
	}
	iss.tok = rotatedTok
	tok, err := c.GetServiceToken(context.Background(), "scope-b")
	if err != nil {
		t.Fatalf("scope-b: %v", err)
	}
	if tok != rotatedTok {
		t.Fatalf("tok=%q (scope changed should miss cache)", tok)
	}
	if got := iss.callCount(); got != 2 {
		t.Fatalf("issuer calls=%d want=2", got)
	}
}

func TestCachedServiceTokenSource_PropagatesError(t *testing.T) {
	iss := &fakeIssuer{err: ErrUpstream}
	c := NewCachedServiceTokenSource(iss, 0)
	_, err := c.GetServiceToken(context.Background(), "s")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err=%v want ErrUpstream", err)
	}
}
