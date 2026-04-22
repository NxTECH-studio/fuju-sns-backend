package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fuju/backend/pkg/auth"
)

const testAccessToken = "at-abc"

func newIssueRequest(exp time.Time) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/session", nil)
	ctx := auth.SetAccessTokenInContext(r.Context(), testAccessToken)
	if !exp.IsZero() {
		ctx = auth.SetExpiresAtInContext(ctx, exp)
	}
	return r.WithContext(ctx)
}

func defaultCookieCfg() SessionCookieConfig {
	return SessionCookieConfig{
		Name:           "fuju_access",
		Secure:         true,
		SameSite:       http.SameSiteLaxMode,
		Path:           "/",
		FallbackMaxAge: time.Hour,
	}
}

func TestSessionHandler_Issue_setsCookieFromContext(t *testing.T) {
	h := NewSessionHandler(defaultCookieCfg())
	rec := httptest.NewRecorder()
	exp := time.Now().Add(30 * time.Minute)

	h.Issue(rec, newIssueRequest(exp))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one Set-Cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "fuju_access" {
		t.Errorf("Name = %q, want fuju_access", c.Name)
	}
	if c.Value != "at-abc" {
		t.Errorf("Value mismatch: %q", c.Value)
	}
	if !c.HttpOnly {
		t.Errorf("expected HttpOnly")
	}
	if !c.Secure {
		t.Errorf("expected Secure")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v", c.SameSite)
	}
	// Max-Age should reflect the remaining time to exp (±2s tolerance).
	want := int(time.Until(exp).Seconds())
	if diff := c.MaxAge - want; diff > 2 || diff < -2 {
		t.Errorf("MaxAge = %d, want ~%d", c.MaxAge, want)
	}
}

func TestSessionHandler_Issue_usesFallbackWhenExpAbsent(t *testing.T) {
	cfg := defaultCookieCfg()
	cfg.FallbackMaxAge = 15 * time.Minute
	h := NewSessionHandler(cfg)

	rec := httptest.NewRecorder()
	h.Issue(rec, newIssueRequest(time.Time{}))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Result().Cookies()[0].MaxAge; got != 900 {
		t.Errorf("MaxAge = %d, want 900 (15m fallback)", got)
	}
}

func TestSessionHandler_Issue_fallsBackOnSubSecondRemaining(t *testing.T) {
	// A remaining lifetime under one second rounds to MaxAge=0, which
	// net/http treats as "session cookie" — the exact opposite of
	// what we want for an expiring auth cookie. The handler must fall
	// back to the configured default instead.
	cfg := defaultCookieCfg()
	cfg.FallbackMaxAge = 5 * time.Minute
	h := NewSessionHandler(cfg)

	rec := httptest.NewRecorder()
	h.Issue(rec, newIssueRequest(time.Now().Add(500*time.Millisecond)))

	got := rec.Result().Cookies()[0].MaxAge
	if got != 300 {
		t.Errorf("MaxAge = %d, want 300 (fallback kicked in at sub-second exp)", got)
	}
}

func TestSessionHandler_Issue_usesFallbackWhenExpAlreadyPassed(t *testing.T) {
	// AuthCore returned a token whose `exp` is in the past (clock skew
	// or stale cache). The handler falls back to the configured default
	// so we don't emit a Set-Cookie with a zero / negative Max-Age.
	cfg := defaultCookieCfg()
	cfg.FallbackMaxAge = 10 * time.Minute
	h := NewSessionHandler(cfg)

	rec := httptest.NewRecorder()
	h.Issue(rec, newIssueRequest(time.Now().Add(-time.Hour)))

	got := rec.Result().Cookies()[0].MaxAge
	if got != 600 {
		t.Errorf("MaxAge = %d, want 600 (fallback applied after past exp)", got)
	}
}

func TestSessionHandler_Issue_500WhenTokenMissing(t *testing.T) {
	// A 500 is the right signal here: missing access token in the
	// context means the middleware chain was mis-wired. Returning 401
	// would hide the bug.
	h := NewSessionHandler(defaultCookieCfg())
	rec := httptest.NewRecorder()
	h.Issue(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/session", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on missing ctx token, got %d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("no Set-Cookie should be emitted on error")
	}
}

func TestSessionHandler_Revoke_returns204AndDeleteCookie(t *testing.T) {
	h := NewSessionHandler(defaultCookieCfg())
	rec := httptest.NewRecorder()

	h.Revoke(rec, httptest.NewRequest(http.MethodDelete, "/v1/auth/session", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one Set-Cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "fuju_access" {
		t.Errorf("Name = %q, want fuju_access", c.Name)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1 (delete)", c.MaxAge)
	}
}

func TestSessionHandler_Revoke_isIdempotent(t *testing.T) {
	h := NewSessionHandler(defaultCookieCfg())
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.Revoke(rec, httptest.NewRequest(http.MethodDelete, "/v1/auth/session", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("iteration %d: expected 204, got %d", i, rec.Code)
		}
	}
}
