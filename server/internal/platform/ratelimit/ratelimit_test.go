package ratelimit

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestLimiter(r rate.Limit, burst int, ttl time.Duration) (*Limiter, *fakeClock) {
	clk := &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	l := New(r, burst, ttl)
	l.now = clk.Now
	return l, clk
}

func TestLimiterAllowsBurstThenBlocks(t *testing.T) {
	l, _ := newTestLimiter(rate.Every(6*time.Second), 10, time.Minute)
	for i := range 10 {
		if ok, _ := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("request %d refused within the burst", i+1)
		}
	}
	ok, retry := l.Allow("1.2.3.4")
	if ok {
		t.Fatal("11th request allowed, want refused")
	}
	if retry <= 0 || retry > 6*time.Second {
		t.Errorf("retryAfter = %v, want (0, 6s]", retry)
	}
	// A refused request does not use up a token: after the refill interval
	// exactly one more request passes.
	if ok, _ := l.Allow("1.2.3.4"); ok {
		t.Fatal("second refused request allowed")
	}
}

func TestLimiterRefills(t *testing.T) {
	l, clk := newTestLimiter(rate.Every(20*time.Minute), 3, time.Hour)
	for range 3 {
		l.Allow("k")
	}
	if ok, retry := l.Allow("k"); ok || retry != 20*time.Minute {
		t.Fatalf("Allow = %v, %v; want refused with 20m", ok, retry)
	}
	clk.Advance(20 * time.Minute)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("not allowed after one refill interval")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("allowed twice after one refill interval")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(rate.Every(time.Minute), 1, time.Hour)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a refused")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b refused after a used its token")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a allowed twice")
	}
}

func TestLimiterEvictsIdleKeys(t *testing.T) {
	l, clk := newTestLimiter(rate.Every(time.Second), 1, time.Minute)
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	if n := l.size(); n != 3 {
		t.Fatalf("size = %d, want 3", n)
	}
	clk.Advance(30 * time.Second)
	l.Allow("c") // keeps c fresh
	clk.Advance(45 * time.Second)
	l.Allow("d") // triggers the sweep: a and b idle for 75 s > ttl
	if n := l.size(); n != 2 {
		t.Fatalf("size after sweep = %d, want 2 (c, d)", n)
	}
}

func TestLimiterConcurrentUse(t *testing.T) {
	l := New(rate.Every(time.Hour), 50, time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 200 {
		wg.Go(func() {
			if ok, _ := l.Allow("same"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 50 {
		t.Fatalf("allowed = %d, want exactly the burst of 50", allowed)
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func TestLimiterCapsKeys(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	l := New(rate.Every(time.Second), 1, time.Minute, WithMaxKeys(2))
	l.now = clk.Now
	l.Allow("a")
	clk.Advance(30 * time.Second)
	l.Allow("b")
	ok, retry := l.Allow("c")
	if ok {
		t.Fatal("a third key was admitted to a full limiter")
	}
	if retry <= 0 || retry > 5*time.Second {
		t.Fatalf("retryAfter = %v, want a short positive delay", retry)
	}
	if n := l.size(); n != 2 {
		t.Fatalf("size = %d, want 2", n)
	}
	// t=61 s: the periodic sweep drops a (idle 61 s) and the call re-admits
	// it; b (idle 31 s) stays.
	clk.Advance(31 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("known key refused while the table is full")
	}
	clk.Advance(34 * time.Second)
	l.Allow("a")
	// t=100 s: the periodic sweep is not due (39 s since the last), but the
	// full table sweeps at once and drops b, idle for 70 s > TTL.
	clk.Advance(5 * time.Second)
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("new key refused although an idle key could be swept")
	}
	if n := l.size(); n != 2 {
		t.Fatalf("size = %d, want 2 (a, c)", n)
	}
}

func TestLimiterDefaultMaxKeys(t *testing.T) {
	if l := New(rate.Every(time.Second), 1, time.Minute); l.maxKeys != DefaultMaxKeys || DefaultMaxKeys != 100_000 {
		t.Fatalf("maxKeys = %d (DefaultMaxKeys %d), want 100000", l.maxKeys, DefaultMaxKeys)
	}
}

func TestLimiterCheckAndCharge(t *testing.T) {
	l, clk := newTestLimiter(rate.Every(90*time.Second), 3, time.Hour)
	for i := range 3 {
		if ok, _ := l.Check("u"); !ok {
			t.Fatalf("check %d refused before any charge", i+1)
		}
		l.Charge("u")
	}
	// Checking alone never uses tokens.
	for range 5 {
		if ok, _ := l.Check("other"); !ok {
			t.Fatal("check consumed tokens")
		}
	}
	ok, retry := l.Check("u")
	if ok || retry != 90*time.Second {
		t.Fatalf("Check after 3 charges = %v, %v; want refused, 90s", ok, retry)
	}
	clk.Advance(90 * time.Second)
	if ok, _ := l.Check("u"); !ok {
		t.Fatal("not refilled after 90s")
	}
}
