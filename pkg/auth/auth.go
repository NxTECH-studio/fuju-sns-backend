// Package auth holds context helpers for propagating the authenticated
// identity through the request handler chain. Token issuance and validation
// live in AuthCore; see pkg/authcore.
package auth

import (
	"context"

	"github.com/fuju/backend/internal/domain"
)

type contextKey string

const (
	subKey         contextKey = "authcore_sub"
	accessTokenKey contextKey = "authcore_access_token"
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
