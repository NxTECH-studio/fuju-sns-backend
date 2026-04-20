package authcore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPClient_Introspect(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
		wantSub string
	}{
		{name: "active", status: http.StatusOK, body: `{"active":true,"sub":"01HX","exp":1800000000,"username":"alice"}`, wantSub: "01HX"},
		{name: "inactive", status: http.StatusOK, body: `{"active":false}`, wantErr: ErrInvalidSession},
		{name: "active_missing_sub", status: http.StatusOK, body: `{"active":true}`, wantErr: ErrUpstream},
		{name: "upstream_500", status: http.StatusInternalServerError, body: ``, wantErr: ErrUpstream},
		{name: "client_creds_rejected", status: http.StatusUnauthorized, body: ``, wantErr: ErrUpstream},
		{name: "unexpected_status", status: http.StatusTeapot, body: ``, wantErr: ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/auth/introspect" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
					t.Errorf("want form content type, got %q", ct)
				}
				if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Basic ") {
					t.Errorf("want Basic auth, got %q", auth)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatalf("parse form: %v", err)
				}
				if tok := r.PostFormValue("token"); tok == "" {
					t.Errorf("missing token field")
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
			session, err := c.Introspect(context.Background(), "token")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want err %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if session.Sub != tc.wantSub {
				t.Errorf("got sub %q, want %q", session.Sub, tc.wantSub)
			}
			if session.PublicID != "alice" {
				t.Errorf("got public id %q", session.PublicID)
			}
		})
	}
}

func TestHTTPClient_Introspect_EmptyToken(t *testing.T) {
	c := New(Options{BaseURL: "http://unused", ClientID: "cid", ClientSecret: "sec"})
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
		ClientID:          "cid",
		ClientSecret:      "sec",
		IntrospectTimeout: 20 * time.Millisecond,
	})
	_, err := c.Introspect(context.Background(), "token")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("expected ErrUpstream on timeout, got %v", err)
	}
}

func TestHTTPClient_GetProfile(t *testing.T) {
	icon := "https://ex.com/a.png"
	cases := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantIcon string
	}{
		{name: "ok", status: http.StatusOK, body: `{"id":"01HX","email":"a@b","public_id":"alice","icon_url":"https://ex.com/a.png"}`, wantIcon: icon},
		{name: "not_found", status: http.StatusNotFound, body: ``, wantErr: ErrNotFound},
		{name: "unauthorized", status: http.StatusUnauthorized, body: ``, wantErr: ErrInvalidSession},
		{name: "upstream_500", status: http.StatusInternalServerError, body: ``, wantErr: ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/user/profile" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				if auth := r.Header.Get("Authorization"); auth != "Bearer tok" {
					t.Errorf("want Bearer tok, got %q", auth)
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
			profile, err := c.GetProfile(context.Background(), "tok")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want err %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if profile.PublicID != "alice" {
				t.Errorf("got public_id %q", profile.PublicID)
			}
			if profile.IconURL != tc.wantIcon {
				t.Errorf("got icon %q", profile.IconURL)
			}
		})
	}
}

func TestIntrospectCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"active": true,
			"sub":    "01HX",
			"exp":    time.Now().Add(1 * time.Hour).Unix(),
		})
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
	cache := NewIntrospectCache(inner, 50*time.Millisecond)

	s1, err := cache.Introspect(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	s2, err := cache.Introspect(context.Background(), "tok")
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
	if _, err := cache.Introspect(context.Background(), "tok"); err != nil {
		t.Fatalf("unexpected err after expiry: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 upstream calls after TTL, got %d", callCount)
	}
}

func TestIntrospectCache_InvalidSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"active":false}`))
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
	cache := NewIntrospectCache(inner, 30*time.Second)
	_, err := cache.Introspect(context.Background(), "tok")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession, got %v", err)
	}
}

func TestIntrospectCache_Singleflight(t *testing.T) {
	var callCount int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"active":true,"sub":"01HX","exp":1800000000}`))
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
	cache := NewIntrospectCache(inner, time.Second)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.Introspect(context.Background(), "same-token"); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 upstream call via singleflight, got %d", callCount)
	}
}

func TestIntrospectCache_SweepExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"active":true,"sub":"01HX","exp":1800000000}`))
	}))
	defer srv.Close()

	inner := New(Options{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec"})
	cache := NewIntrospectCache(inner, 10*time.Millisecond)
	if _, err := cache.Introspect(context.Background(), "tok"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	cache.SweepExpired()
	cache.mu.Lock()
	n := len(cache.items)
	cache.mu.Unlock()
	if n != 0 {
		t.Errorf("expected sweep to remove expired entries, got %d left", n)
	}
}
