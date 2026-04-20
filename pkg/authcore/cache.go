package authcore

import (
	"context"
	"sync"
	"time"
)

// IntrospectCache is a short-lived in-memory cache for introspection
// results. Its TTL (default 30s) sits well below any session revocation SLA
// we care about, and in exchange it absorbs per-request traffic spikes so
// AuthCore is not hit on every SNS API call.
type IntrospectCache struct {
	inner Client
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	items map[string]cacheEntry
}

type cacheEntry struct {
	session   *Session
	fetchedAt time.Time
}

// NewIntrospectCache wraps the given Client with a fixed-TTL memoisation
// layer.
func NewIntrospectCache(inner Client, ttl time.Duration) *IntrospectCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &IntrospectCache{
		inner: inner,
		ttl:   ttl,
		now:   time.Now,
		items: make(map[string]cacheEntry),
	}
}

// Introspect returns the cached Session when still fresh, otherwise delegates
// to the wrapped client.
func (c *IntrospectCache) Introspect(ctx context.Context, sessionToken string) (*Session, error) {
	now := c.now()

	c.mu.Lock()
	if entry, ok := c.items[sessionToken]; ok {
		if c.isFresh(entry, now) {
			session := *entry.session
			c.mu.Unlock()
			return &session, nil
		}
		delete(c.items, sessionToken)
	}
	c.mu.Unlock()

	session, err := c.inner.Introspect(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.items[sessionToken] = cacheEntry{session: session, fetchedAt: now}
	c.mu.Unlock()

	result := *session
	return &result, nil
}

// GetProfile passes through to the wrapped client (profile caching lives in
// the users table, not here).
func (c *IntrospectCache) GetProfile(ctx context.Context, sub string) (*Profile, error) {
	return c.inner.GetProfile(ctx, sub)
}

// Invalidate drops a cached entry (useful for logout flows).
func (c *IntrospectCache) Invalidate(sessionToken string) {
	c.mu.Lock()
	delete(c.items, sessionToken)
	c.mu.Unlock()
}

// SweepExpired removes entries older than the TTL. Intended for a goroutine
// loop in long-running processes.
func (c *IntrospectCache) SweepExpired() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.items {
		if !c.isFresh(v, now) {
			delete(c.items, k)
		}
	}
}

func (c *IntrospectCache) isFresh(entry cacheEntry, now time.Time) bool {
	if now.Sub(entry.fetchedAt) >= c.ttl {
		return false
	}
	if !entry.session.ExpiresAt.IsZero() && !now.Before(entry.session.ExpiresAt) {
		return false
	}
	return true
}
