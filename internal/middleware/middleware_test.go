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
	mw := AuthMiddleware(stub)
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
	mw := AuthMiddleware(stub)
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
	mw := AuthMiddleware(stub)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, newBearerRequest("bad"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_UpstreamDown(t *testing.T) {
	stub := &stubClient{err: authcore.ErrUpstream}
	mw := AuthMiddleware(stub)
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, newBearerRequest("any"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Success(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: time.Now().Add(time.Hour)}}
	mw := AuthMiddleware(stub)
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
