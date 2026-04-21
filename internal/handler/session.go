package handler

import (
	"net/http"
	"time"

	"github.com/fuju/backend/pkg/auth"
	"github.com/fuju/backend/pkg/cookie"
)

// SessionCookieConfig describes the Set-Cookie attributes used by the
// handoff endpoints. Values come from config.Config at wiring time;
// the handler carries them so tests can stand up a SessionHandler
// without reaching into global state.
type SessionCookieConfig struct {
	Name           string
	Secure         bool
	SameSite       http.SameSite
	Domain         string
	Path           string // empty → "/"
	FallbackMaxAge time.Duration
}

// SessionHandler serves the Cookie-based session handoff for browsers.
// POST exchanges a validated Bearer for an HttpOnly cookie; DELETE
// clears the cookie unconditionally.
type SessionHandler struct {
	cfg SessionCookieConfig
}

// NewSessionHandler constructs a SessionHandler with the supplied
// cookie configuration.
func NewSessionHandler(cfg SessionCookieConfig) *SessionHandler {
	return &SessionHandler{cfg: cfg}
}

// Issue handles POST /v1/auth/session. By the time this runs
// AuthMiddleware has already introspected the Bearer, so we just read
// the token + expiry from the context and flip them into a Set-Cookie.
// The body is intentionally empty: the cookie itself is the response.
func (h *SessionHandler) Issue(w http.ResponseWriter, r *http.Request) {
	token, ok := auth.GetAccessTokenFromContext(r.Context())
	if !ok {
		// The only way to hit this branch is a misconfigured middleware
		// chain that stripped the token before reaching the handler.
		// Surface as a 500 so operators notice the wiring bug.
		http.Error(w, "internal: access token missing from context", http.StatusInternalServerError)
		return
	}

	maxAge := h.cfg.FallbackMaxAge
	if exp, ok := auth.GetExpiresAtFromContext(r.Context()); ok {
		if remaining := time.Until(exp); remaining > 0 {
			maxAge = remaining
		}
	}

	http.SetCookie(w, cookie.Build(cookie.Attrs{
		Name:     h.cfg.Name,
		Value:    token,
		MaxAge:   maxAge,
		Secure:   h.cfg.Secure,
		SameSite: h.cfg.SameSite,
		Domain:   h.cfg.Domain,
		Path:     h.cfg.Path,
	}))
	w.WriteHeader(http.StatusNoContent)
}

// Revoke handles DELETE /v1/auth/session. The route is public and
// idempotent so a stale tab that no longer has a valid AuthCore token
// can still clear its browser cookie; we do not require authentication
// and respond 204 even when no cookie was present.
func (h *SessionHandler) Revoke(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, cookie.Build(cookie.Attrs{
		Name:     h.cfg.Name,
		Value:    "",
		MaxAge:   -1,
		Secure:   h.cfg.Secure,
		SameSite: h.cfg.SameSite,
		Domain:   h.cfg.Domain,
		Path:     h.cfg.Path,
	}))
	w.WriteHeader(http.StatusNoContent)
}
