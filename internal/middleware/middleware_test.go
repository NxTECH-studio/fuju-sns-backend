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

func newAuthedRequest(cookieValue string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "authcore_session", Value: cookieValue})
	}
	return req
}

func TestAuthMiddleware_NoCookie(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX"}}
	mw := AuthMiddleware(stub, "")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("next should not be called")
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidSession(t *testing.T) {
	stub := &stubClient{err: authcore.ErrInvalidSession}
	mw := AuthMiddleware(stub, "")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("next should not be called")
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, newAuthedRequest("bad"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_UpstreamDown(t *testing.T) {
	stub := &stubClient{err: authcore.ErrUpstream}
	mw := AuthMiddleware(stub, "")
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, newAuthedRequest("any"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestAuthMiddleware_Success(t *testing.T) {
	stub := &stubClient{session: &authcore.Session{Sub: "01HX", ExpiresAt: time.Now().Add(time.Hour)}}
	mw := AuthMiddleware(stub, "")
	var gotSub string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sub, ok := auth.GetSubFromContext(r.Context())
		if !ok {
			t.Error("sub not set")
		}
		gotSub = sub
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, newAuthedRequest("good"))
	if gotSub != "01HX" {
		t.Errorf("expected sub 01HX, got %q", gotSub)
	}
}
