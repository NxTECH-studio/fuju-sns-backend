// Package cookie centralizes Set-Cookie assembly for the backend. The
// shape is intentionally minimal: handlers own the cookie value and
// pass it together with policy knobs (Secure / SameSite / Domain /
// Max-Age) that come from config. Keeping the policy→*http.Cookie
// conversion in one place avoids drift between issue and revoke paths.
package cookie

import (
	"fmt"
	"net/http"
	"time"
)

// Attrs captures the policy knobs that drive a single Set-Cookie.
// MaxAge handling mirrors net/http:
//   - MaxAge > 0 → cookie expires after MaxAge
//   - MaxAge == 0 → session cookie (no Max-Age attribute emitted)
//   - MaxAge < 0 → cookie is deleted immediately (Max-Age=0 / -1 wire form)
type Attrs struct {
	Name     string
	Value    string
	MaxAge   time.Duration
	Secure   bool
	SameSite http.SameSite
	Domain   string
	Path     string
}

// Build converts Attrs into an *http.Cookie ready for http.SetCookie.
// A zero Path defaults to "/" so callers don't have to remember that
// net/http would otherwise scope the cookie to the request URL's
// directory (which is never what we want for app-wide auth cookies).
func Build(a Attrs) *http.Cookie {
	path := a.Path
	if path == "" {
		path = "/"
	}

	maxAge := int(a.MaxAge.Seconds())
	if a.MaxAge < 0 {
		// net/http treats any negative value as "delete now"; normalise
		// to -1 so the wire form is stable regardless of input scale.
		maxAge = -1
	}

	return &http.Cookie{
		Name:     a.Name,
		Value:    a.Value,
		Path:     path,
		Domain:   a.Domain,
		MaxAge:   maxAge,
		Secure:   a.Secure,
		HttpOnly: true,
		SameSite: a.SameSite,
	}
}

// ParseSameSite turns the string form used in config/env into
// http.SameSite. Unknown values return http.SameSiteLaxMode and a
// non-nil error so the caller can surface validation failures at boot
// rather than silently picking a default at request time.
func ParseSameSite(s string) (http.SameSite, error) {
	switch s {
	case "Lax":
		return http.SameSiteLaxMode, nil
	case "Strict":
		return http.SameSiteStrictMode, nil
	case "None":
		return http.SameSiteNoneMode, nil
	default:
		return http.SameSiteLaxMode, fmt.Errorf("cookie: unknown SameSite value %q (want Lax, Strict, or None)", s)
	}
}
