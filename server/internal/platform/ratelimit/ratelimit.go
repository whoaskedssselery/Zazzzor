// Package ratelimit limits how often a key (usually a client IP) may do
// something, with one token bucket per key.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// DefaultMaxKeys bounds the number of buckets a Limiter keeps.
const DefaultMaxKeys = 100_000

// fullRetryAfter is what a new key is told to wait while the table is full.
const fullRetryAfter = time.Second

// Limiter is a set of token buckets keyed by string. Buckets idle for longer
// than the TTL are dropped; choose a TTL at least as long as a full refill
// (burst / rate) so that dropping a bucket never hands out extra tokens. The
// number of buckets is capped (DefaultMaxKeys unless WithMaxKeys): when the
// table is full, idle buckets are swept at once, and if none can go a new key
// is refused. It is safe for concurrent use.
type Limiter struct {
	rate    rate.Limit
	burst   int
	ttl     time.Duration
	maxKeys int
	now     func() time.Time

	mu         sync.Mutex
	buckets    map[string]*bucket
	lastSweep  time.Time
	lastForced time.Time
}

type bucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
	inFlight int
}

// Option configures a Limiter.
type Option func(*Limiter)

// WithMaxKeys caps the number of tracked keys (minimum 1).
func WithMaxKeys(n int) Option {
	return func(l *Limiter) { l.maxKeys = max(n, 1) }
}

// WithClock sets the clock used for refill and expiry. Nil keeps the default.
func WithClock(now func() time.Time) Option {
	return func(l *Limiter) {
		if now != nil {
			l.now = now
		}
	}
}

// New returns a limiter allowing burst events at once per key, refilled at r.
func New(r rate.Limit, burst int, ttl time.Duration, opts ...Option) *Limiter {
	l := &Limiter{rate: r, burst: burst, ttl: ttl, maxKeys: DefaultMaxKeys, now: time.Now,
		buckets: make(map[string]*bucket)}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Allow takes a token for key. When none is left it returns false and how
// long until one is; a refused call does not use up a token. A new key is
// refused (with a short delay) while the table is full.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now, false)

	b := l.bucketFor(key, now)
	if b == nil {
		return false, fullRetryAfter
	}
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

// Check reports whether key has a token left, without taking it; use it with
// Charge to count only some outcomes (e.g. failed logins). Unknown keys have
// a full bucket.
func (l *Limiter) Check(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now, false)

	b := l.buckets[key]
	if b == nil {
		return true, 0
	}
	res := b.lim.ReserveN(now, 1)
	defer res.CancelAt(now)
	if !res.OK() {
		return false, l.ttl
	}
	if d := res.DelayFrom(now); d > 0 {
		return false, d
	}
	return true, 0
}

// Charge takes a token for key even when none is left, so concurrent
// outcomes that all passed Check still each count (the bucket goes into
// debt). While the table is full and nothing can be swept, a new key is not
// tracked.
func (l *Limiter) Charge(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now, false)
	if b := l.bucketFor(key, now); b != nil {
		b.lim.ReserveN(now, 1)
	}
}

// BeginAttempt reserves room for an in-flight operation. Call finish once
// when it ends, passing true to charge a failure. Successful attempts
// release their reservation immediately without consuming a token.
// Use this for keys whose outcomes are only counted on failure.
func (l *Limiter) BeginAttempt(key string) (finish func(charge bool), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now, false)
	b := l.bucketFor(key, now)
	if b == nil || b.lim.TokensAt(now) < float64(b.inFlight+1) {
		return nil, false
	}
	b.inFlight++
	return func(charge bool) {
		l.mu.Lock()
		defer l.mu.Unlock()
		now := l.now()
		b.inFlight--
		b.lastSeen = now
		if charge {
			b.lim.ReserveN(now, 1)
		}
	}, true
}

// bucketFor returns key's bucket, creating it if there is room; nil when the
// table is full. l.mu must be held.
func (l *Limiter) bucketFor(key string, now time.Time) *bucket {
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxKeys {
			// A full sweep walks every key; under a flood of new keys do it
			// at most once per fullRetryAfter.
			if now.Sub(l.lastForced) >= fullRetryAfter {
				l.lastForced = now
				l.sweep(now, true)
			}
			if len(l.buckets) >= l.maxKeys {
				return nil
			}
		}
		b = &bucket{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	return b
}

// sweep drops idle buckets, at most once per TTL unless forced. l.mu must be
// held.
func (l *Limiter) sweep(now time.Time, force bool) {
	if !force && now.Sub(l.lastSweep) < l.ttl {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if b.inFlight == 0 && now.Sub(b.lastSeen) > l.ttl {
			delete(l.buckets, k)
		}
	}
}
