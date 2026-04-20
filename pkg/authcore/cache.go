package authcore

import (
	"context"
	"sync"
	"time"
)

// IntrospectCache wraps a Client with a short-lived in-memory cache and
// singleflight dedup. Its TTL (default 30s) sits below any reasonable
// revocation SLA while absorbing per-request traffic spikes so AuthCore is
// not hit on every SNS API call.
type IntrospectCache struct {
	inner Client
	ttl   time.Duration
	now   func() time.Time

	mu       sync.Mutex
	items    map[string]cacheEntry
	inFlight map[string]*introspectCall
}

type cacheEntry struct {
	session   *Session
	fetchedAt time.Time
}

// introspectCall holds the shared result of a concurrent upstream call so N
// goroutines asking for the same unknown token collapse into one request.
type introspectCall struct {
	done    chan struct{}
	session *Session
	err     error
}

// NewIntrospectCache wraps the given Client with a fixed-TTL memoisation
// layer. ttl<=0 uses 30s.
func NewIntrospectCache(inner Client, ttl time.Duration) *IntrospectCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &IntrospectCache{
		inner:    inner,
		ttl:      ttl,
		now:      time.Now,
		items:    make(map[string]cacheEntry),
		inFlight: make(map[string]*introspectCall),
	}
}

// Introspect returns the cached Session when still fresh, or delegates to
// the wrapped client (with singleflight-style dedup).
func (c *IntrospectCache) Introspect(ctx context.Context, accessToken string) (*Session, error) {
	now := c.now()

	c.mu.Lock()
	if entry, ok := c.items[accessToken]; ok {
		if c.isFresh(entry, now) {
			session := *entry.session
			c.mu.Unlock()
			return &session, nil
		}
		delete(c.items, accessToken)
	}

	if call, ok := c.inFlight[accessToken]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			if call.err != nil {
				return nil, call.err
			}
			copy := *call.session
			return &copy, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	call := &introspectCall{done: make(chan struct{})}
	c.inFlight[accessToken] = call
	c.mu.Unlock()

	call.session, call.err = c.inner.Introspect(ctx, accessToken)

	c.mu.Lock()
	delete(c.inFlight, accessToken)
	if call.err == nil && call.session != nil {
		c.items[accessToken] = cacheEntry{session: call.session, fetchedAt: now}
	}
	c.mu.Unlock()
	close(call.done)

	if call.err != nil {
		return nil, call.err
	}
	result := *call.session
	return &result, nil
}

// GetProfile passes through to the wrapped client (profile caching lives in
// the users table, not here).
func (c *IntrospectCache) GetProfile(ctx context.Context, accessToken string) (*Profile, error) {
	return c.inner.GetProfile(ctx, accessToken)
}

// Invalidate drops a cached entry (useful for logout flows).
func (c *IntrospectCache) Invalidate(accessToken string) {
	c.mu.Lock()
	delete(c.items, accessToken)
	c.mu.Unlock()
}

// SweepExpired removes entries older than the TTL. Call periodically from a
// ticker to bound memory.
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

// StartJanitor spawns a goroutine that sweeps expired entries at the given
// interval until ctx is cancelled. interval<=0 uses ttl.
func (c *IntrospectCache) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = c.ttl
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.SweepExpired()
			}
		}
	}()
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
