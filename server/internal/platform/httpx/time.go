package httpx

import "time"

// TimeLayout is how timestamps appear in JSON: RFC 3339 in UTC with
// milliseconds, e.g. 2026-10-21T12:00:00.000Z.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTime renders t in TimeLayout.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }
