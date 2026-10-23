package snowflake

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeneratorMonotonicUnderConcurrency(t *testing.T) {
	const goroutines, perG = 8, 10_000
	g := NewGenerator(0, time.Now)

	results := make([][]ID, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			ids := make([]ID, perG)
			for j := range ids {
				ids[j] = g.Next()
			}
			results[i] = ids
		})
	}
	wg.Wait()

	seen := make(map[ID]struct{}, goroutines*perG)
	for i, ids := range results {
		for j, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate id %d", id)
			}
			seen[id] = struct{}{}
			if j > 0 && id <= ids[j-1] {
				t.Fatalf("goroutine %d: id[%d]=%d <= id[%d]=%d", i, j, id, j-1, ids[j-1])
			}
		}
	}
}

func TestIDEncodesTimeSinceEpoch(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	g := NewGenerator(0, func() time.Time { return at })
	id := g.Next()
	if got := id.Time(); !got.Equal(at) {
		t.Fatalf("Time() = %v, want %v", got, at)
	}
	if got := int64(id) >> 22; got != 1000 {
		t.Fatalf("timestamp bits = %d, want 1000 ms", got)
	}
	if got := id.Time().Location(); got != time.UTC {
		t.Errorf("Time() location = %v, want UTC", got)
	}
}

func TestIDEncodesWorkerAndSequence(t *testing.T) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	g := NewGenerator(5, func() time.Time { return at })
	a, b := g.Next(), g.Next()
	if w := (int64(a) >> 12) & 0x3FF; w != 5 {
		t.Errorf("worker bits = %d, want 5", w)
	}
	if s := int64(a) & 0xFFF; s != 0 {
		t.Errorf("first sequence = %d, want 0", s)
	}
	if b != a+1 {
		t.Errorf("second id in the same ms = %d, want %d", b, a+1)
	}
}

func TestNewGeneratorRejectsBigWorker(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewGenerator(1024) did not panic")
		}
	}()
	NewGenerator(1024, time.Now)
}

func TestIDJSONIsString(t *testing.T) {
	b, err := json.Marshal(ID(42))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"42"` {
		t.Fatalf("Marshal = %s, want \"42\"", b)
	}

	var v struct {
		ID ID `json:"id"`
	}
	if err := json.Unmarshal([]byte(`{"id":"9007199254740993"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.ID != 9007199254740993 {
		t.Fatalf("Unmarshal = %d", v.ID)
	}
	if got := v.ID.String(); got != "9007199254740993" {
		t.Errorf("String() = %q", got)
	}

	for _, bad := range []string{`"abc"`, `42`, `""`, `"-1"`, `"1.5"`, `" 1"`, `"99999999999999999999"`} {
		var id ID
		if err := json.Unmarshal([]byte(bad), &id); err == nil {
			t.Errorf("Unmarshal(%s) = %d, want error", bad, id)
		}
	}
}

func TestParse(t *testing.T) {
	id, err := Parse("123")
	if err != nil || id != 123 {
		t.Fatalf("Parse(123) = %d, %v", id, err)
	}
	for _, bad := range []string{"", "abc", "-5", "+5", "0x10", "1e3"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded", bad)
		}
	}
}

func TestGeneratorWaitsOnClockRollback(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	// The clock reads base, then jumps 5 ms back for a few reads, then
	// recovers to base+1ms.
	now := func() time.Time {
		switch n := calls.Add(1); {
		case n == 1:
			return base
		case n <= 6:
			return base.Add(-5 * time.Millisecond)
		default:
			return base.Add(time.Millisecond)
		}
	}
	g := NewGenerator(0, now)
	first := g.Next()
	second := g.Next()
	if second <= first {
		t.Fatalf("id after rollback %d <= previous %d", second, first)
	}
	if !second.Time().Equal(base.Add(time.Millisecond)) {
		t.Fatalf("id after rollback has time %v, want %v (generator must wait, not reuse the past)",
			second.Time(), base.Add(time.Millisecond))
	}
	if calls.Load() < 7 {
		t.Fatalf("clock read %d times; generator did not wait for it to catch up", calls.Load())
	}
}

func TestGeneratorSequenceOverflowWaitsForNextMillisecond(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	now := func() time.Time {
		// 4096 IDs fit into one millisecond; the clock moves on only after
		// a few extra reads.
		if calls.Add(1) <= 4100 {
			return base
		}
		return base.Add(time.Millisecond)
	}
	g := NewGenerator(0, now)
	var last ID
	for i := range 4097 {
		id := g.Next()
		if i > 0 && id <= last {
			t.Fatalf("id %d not increasing", i)
		}
		last = id
	}
	if !last.Time().Equal(base.Add(time.Millisecond)) {
		t.Fatalf("4097th id time = %v, want next millisecond", last.Time())
	}
}
