package messages

import (
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
)

// burstAndRefill drains l for one key and returns how many calls passed and
// how long the first refused one was told to wait.
func burstAndRefill(l *ratelimit.Limiter) (int, time.Duration) {
	n := 0
	for {
		ok, retry := l.Allow("k")
		if !ok {
			return n, retry
		}
		n++
	}
}

// The default limits: sending 5/s with bursts of 10, edits and deletions
// 10/s with bursts of 20, typing calls 5/s with bursts of 10, one typing
// notice per 3 s.
func TestDefaultLimits(t *testing.T) {
	t.Parallel()
	s := NewService(Options{})
	for _, tc := range []struct {
		name  string
		l     *ratelimit.Limiter
		burst int
		every time.Duration
	}{
		{"send", s.sendLimiter, 10, time.Second / 5},
		{"write", s.writeLimiter, 20, time.Second / 10},
		{"typing calls", s.typingCalls, 10, time.Second / 5},
		{"typing notice", s.typingLimiter, 1, TypingInterval},
	} {
		n, retry := burstAndRefill(tc.l)
		// The drain takes microseconds; allow a generous margin anyway.
		if n != tc.burst || retry > tc.every || retry < tc.every*3/4 {
			t.Errorf("%s: burst %d, retry %v; want %d, about %v", tc.name, n, retry, tc.burst, tc.every)
		}
	}
}
