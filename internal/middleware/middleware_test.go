package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/authcore"
)

type stubClient struct {
	session *authcore.Session
	err     error
	calls   int
}

func (s *stubClient) Introspect(_ context.Context, _ string) (*authcore.Session, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	sess := *s.session
	return &sess, nil
}

func (s *stubClient) GetProfile(_ context.Context, _ string) (*authcore.Profile, error) {
	return nil, nil
}

func newBearerRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestAuthMiddleware_NoHeader(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX"}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{})
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next should not be called")
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_BadHeader(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX"}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidSession(t *testing.T) {
	stub := &stubClient{err: authcore.ErrInvalidSession}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{})
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, newBearerRequest("bad"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_UpstreamDown(t *testing.T) {
	stub := &stubClient{err: authcore.ErrUpstream}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{})
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, newBearerRequest("any"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Success(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: time.Now().Add(time.Hour)}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{})
	var gotSub, gotToken string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sub, ok := auth.GetSubFromContext(r.Context())
		if !ok {
			t.Error("sub not set")
		}
		gotSub = sub
		tok, ok := auth.GetAccessTokenFromContext(r.Context())
		if !ok {
			t.Error("access token not set")
		}
		gotToken = tok
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, newBearerRequest("good-token"))
	if gotSub != "01HX" {
		t.Errorf("expected sub 01HX, got %q", gotSub)
	}
	if gotToken != "good-token" {
		t.Errorf("expected access token propagated, got %q", gotToken)
	}
}

func newCookieRequest(name, value string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if value != "" {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return req
}

func TestAuthMiddleware_CookieOnly(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: time.Now().Add(time.Hour)}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{CookieName: "fuju_access"})

	var gotSub, gotToken string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotSub, _ = auth.GetSubFromContext(r.Context())
		gotToken, _ = auth.GetAccessTokenFromContext(r.Context())
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, newCookieRequest("fuju_access", "cookie-token"))

	if gotSub != "01HX" {
		t.Errorf("expected sub from cookie path, got %q", gotSub)
	}
	if gotToken != "cookie-token" {
		t.Errorf("expected cookie value as token, got %q", gotToken)
	}
}

func TestAuthMiddleware_HeaderWinsOverCookie(t *testing.T) {
	// Explicit beats implicit: a CLI that wants to override a stale
	// cookie must be able to.
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: time.Now().Add(time.Hour)}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{CookieName: "fuju_access"})

	req := newBearerRequest("header-token")
	req.AddCookie(&http.Cookie{Name: "fuju_access", Value: "cookie-token"})

	var gotToken string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotToken, _ = auth.GetAccessTokenFromContext(r.Context())
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)

	if gotToken != "header-token" {
		t.Errorf("expected header-token to win, got %q", gotToken)
	}
}

func TestAuthMiddleware_UnknownCookieNameIgnored(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX"}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{CookieName: "fuju_access"})

	// Cookie is present but under a different name; middleware must
	// treat it as "no token" rather than silently letting the wrong
	// value through.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "other_cookie", Value: "whatever"})

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_CookieDisabledWhenNameEmpty(t *testing.T) {
	// With CookieName="" the cookie path is disabled regardless of
	// what the request carries. Useful for Bearer-only deployments.
	stub := &stubClient{session: &authcore.Session{Sub: "01HX"}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{}) // CookieName empty
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, newCookieRequest("fuju_access", "cookie-token"))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with cookie path disabled, got %d", rec.Code)
	}
}

func TestAuthMiddleware_PopulatesExpiresAtFromSession(t *testing.T) {
	exp := time.Now().Add(45 * time.Minute).Truncate(time.Second)
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: exp}}
	mw := AuthMiddleware(stub, AuthMiddlewareConfig{CookieName: "fuju_access"})

	var got time.Time
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = auth.GetExpiresAtFromContext(r.Context())
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, newBearerRequest("any"))

	if !got.Equal(exp) {
		t.Errorf("expected ExpiresAt=%v propagated, got %v", exp, got)
	}
}
