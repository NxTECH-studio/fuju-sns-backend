// Package auth holds context helpers for propagating the authenticated
// identity through the request handler chain. Token issuance and validation
// live in AuthCore; see pkg/authcore.
package auth

import (
	"context"
	"time"

	"github.com/fuju/backend/internal/domain"
)

type contextKey string

const (
	subKey         contextKey = "authcore_sub"
	accessTokenKey contextKey = "authcore_access_token"
	expiresAtKey   contextKey = "authcore_expires_at"
	currentUserKey contextKey = "authcore_current_user"
)

// SetSubInContext stores the authenticated AuthCore sub on the context.
func SetSubInContext(ctx context.Context, sub string) context.Context {
	return context.WithValue(ctx, subKey, sub)
}

// GetSubFromContext retrieves the authenticated sub from the context.
func GetSubFromContext(ctx context.Context) (string, bool) {
	sub, ok := ctx.Value(subKey).(string)
	if !ok || sub == "" {
		return "", false
	}
	return sub, true
}

// SetAccessTokenInContext stores the caller's AuthCore access token so later
// stages (hydrate) can call AuthCore on the user's behalf.
func SetAccessTokenInContext(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, accessTokenKey, token)
}

// GetAccessTokenFromContext returns the caller's AuthCore access token.
func GetAccessTokenFromContext(ctx context.Context) (string, bool) {
	tok, ok := ctx.Value(accessTokenKey).(string)
	if !ok || tok == "" {
		return "", false
	}
	return tok, true
}

// SetExpiresAtInContext stores the access token's `exp` claim so that
// downstream handlers (e.g. SessionHandler) can align the Set-Cookie
// Max-Age with the token lifetime without calling AuthCore a second
// time. A zero time encodes "AuthCore did not return exp".
func SetExpiresAtInContext(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, expiresAtKey, t)
}

// GetExpiresAtFromContext returns the access token's `exp` time.
// The bool is false when no value was stored or when the stored time
// is the zero value; callers should fall back to a configured default.
func GetExpiresAtFromContext(ctx context.Context) (time.Time, bool) {
	t, ok := ctx.Value(expiresAtKey).(time.Time)
	if !ok || t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// SetCurrentUserInContext stores the hydrated User on the context so
// downstream handlers can avoid a second lookup.
func SetCurrentUserInContext(ctx context.Context, user *domain.User) context.Context {
	return context.WithValue(ctx, currentUserKey, user)
}

// GetCurrentUserFromContext returns the hydrated User if present.
func GetCurrentUserFromContext(ctx context.Context) (*domain.User, bool) {
	user, ok := ctx.Value(currentUserKey).(*domain.User)
	if !ok || user == nil {
		return nil, false
	}
	return user, true
}
