package httpx_test

import (
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

func TestFormatTimeIsUTCWithMillis(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	got := httpx.FormatTime(time.Date(2026, 10, 21, 15, 4, 5, 6_789_000, msk))
	if got != "2026-10-21T12:04:05.006Z" {
		t.Fatalf("FormatTime = %q", got)
	}
}
