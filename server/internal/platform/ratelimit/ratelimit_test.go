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
