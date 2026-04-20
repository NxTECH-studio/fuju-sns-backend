package authcore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClient_Introspect(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "ok", status: http.StatusOK, body: `{"sub":"01HX","expires_at":"2026-04-20T12:00:00Z"}`},
		{name: "unauthorized", status: http.StatusUnauthorized, body: ``, wantErr: ErrInvalidSession},
		{name: "upstream_500", status: http.StatusInternalServerError, body: ``, wantErr: ErrUpstream},
		{name: "unexpected_status", status: http.StatusTeapot, body: ``, wantErr: ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/introspect" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer svc" {
					t.Errorf("missing or wrong auth header")
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, ServiceToken: "svc"})
			session, err := c.Introspect(context.Background(), "cookie")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want err %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if session.Sub != "01HX" {
				t.Errorf("got sub %q", session.Sub)
			}
		})
	}
}

func TestHTTPClient_Introspect_EmptyToken(t *testing.T) {
	c := New(Options{BaseURL: "http://unused", ServiceToken: "svc"})
	_, err := c.Introspect(context.Background(), "")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}

func TestHTTPClient_Introspect_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Options{
		BaseURL:           srv.URL,
		ServiceToken:      "svc",
		IntrospectTimeout: 20 * time.Millisecond,
	})
	_, err := c.Introspect(context.Background(), "cookie")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("expected ErrUpstream on timeout, got %v", err)
	}
}

func TestHTTPClient_GetProfile(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "ok", status: http.StatusOK, body: `{"sub":"01HX","display_name":"Alice","display_id":"alice","icon_url":"https://ex.com/a.png"}`},
		{name: "not_found", status: http.StatusNotFound, body: ``, wantErr: ErrNotFound},
		{name: "upstream_500", status: http.StatusInternalServerError, body: ``, wantErr: ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/users/01HX" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, ServiceToken: "svc"})
			profile, err := c.GetProfile(context.Background(), "01HX")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want err %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if profile.DisplayName != "Alice" {
				t.Errorf("got display_name %q", profile.DisplayName)
			}
		})
	}
}

func TestIntrospectCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Session{Sub: "01HX", ExpiresAt: time.Now().Add(1 * time.Hour)})
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ServiceToken: "svc"})
	cache := NewIntrospectCache(inner, 50*time.Millisecond)

	s1, err := cache.Introspect(context.Background(), "cookie")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	s2, err := cache.Introspect(context.Background(), "cookie")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if s1.Sub != s2.Sub {
		t.Errorf("cache should return identical sub")
	}
	if callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", callCount)
	}

	time.Sleep(60 * time.Millisecond)
	if _, err := cache.Introspect(context.Background(), "cookie"); err != nil {
		t.Fatalf("unexpected err after expiry: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 upstream calls after TTL, got %d", callCount)
	}
}

func TestIntrospectCache_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ServiceToken: "svc"})
	cache := NewIntrospectCache(inner, 30*time.Second)
	_, err := cache.Introspect(context.Background(), "cookie")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}
