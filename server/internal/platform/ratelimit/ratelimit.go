// Package ratelimit limits how often a key (usually a client IP) may do
// something, with one token bucket per key.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a set of token buckets keyed by string. Buckets idle for longer
// than the TTL are dropped; choose a TTL at least as long as a full refill
// (burst / rate) so that dropping a bucket never hands out extra tokens. It
// is safe for concurrent use.
type Limiter struct {
	rate  rate.Limit
	burst int
	ttl   time.Duration
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// New returns a limiter allowing burst events at once per key, refilled at r.
func New(r rate.Limit, burst int, ttl time.Duration) *Limiter {
	return &Limiter{rate: r, burst: burst, ttl: ttl, now: time.Now, buckets: make(map[string]*bucket)}
}

// Allow takes a token for key. When none is left it returns false and how
// long until one is; a refused call does not use up a token.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	b := l.buckets[key]
	if b == nil {
		b = &bucket{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	res := b.lim.ReserveN(now, 1)
	if !res.OK() {
		// Only when burst is 0: nothing will ever pass.
		return false, l.ttl
	}
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return false, d
	}
	return true, 0
}

// sweep drops idle buckets, at most once per TTL. l.mu must be held.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.ttl {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.ttl {
			delete(l.buckets, k)
		}
	}
}
