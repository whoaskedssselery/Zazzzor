// Package snowflake generates 64-bit, time-ordered IDs.
//
// Layout, from the most significant bit: 1 unused sign bit, 41 bits of
// milliseconds since Epoch, 10 bits of worker, 12 bits of per-millisecond
// sequence. In JSON an ID is a decimal string, so JavaScript clients never
// lose precision.
package snowflake

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Epoch is the zero point of the timestamp bits.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const (
	workerBits   = 10
	sequenceBits = 12
	timeBits     = 41

	// MaxWorker is the largest worker number that fits into an ID.
	MaxWorker   = 1<<workerBits - 1
	maxSequence = 1<<sequenceBits - 1
	maxMillis   = 1<<timeBits - 1
	timeShift   = workerBits + sequenceBits
)

// ID is a snowflake identifier.
type ID int64

// String returns the decimal form of id.
func (id ID) String() string { return strconv.FormatInt(int64(id), 10) }

// Time returns the moment encoded in id, in UTC, at millisecond precision.
func (id ID) Time() time.Time {
	return Epoch.Add(time.Duration(int64(id)>>timeShift) * time.Millisecond)
}

// MarshalJSON encodes id as a JSON string.
func (id ID) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, id.String()), nil
}

// UnmarshalJSON accepts only a JSON string holding a decimal ID; JSON null
// leaves id unchanged, as encoding/json does for other types.
func (id *ID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return errors.New("snowflake: id must be a JSON string")
	}
	v, err := Parse(string(b[1 : len(b)-1]))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

// ErrInvalid is returned by Parse for anything that is not a decimal ID.
var ErrInvalid = errors.New("snowflake: invalid id")

// Parse reads a decimal ID: digits only, no sign, within int64.
func Parse(s string) (ID, error) {
	if s == "" || len(s) > 19 {
		return 0, ErrInvalid
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, ErrInvalid
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return ID(v), nil
}

// Generator hands out unique, strictly increasing IDs. It is safe for
// concurrent use.
type Generator struct {
	worker int64
	now    func() time.Time

	mu       sync.Mutex
	lastMs   int64
	sequence int64
}

// NewGenerator returns a generator for worker (0..MaxWorker) reading the
// clock through now. It panics on a worker that does not fit.
func NewGenerator(worker uint16, now func() time.Time) *Generator {
	if worker > MaxWorker {
		panic(fmt.Sprintf("snowflake: worker %d > %d", worker, MaxWorker))
	}
	return &Generator{worker: int64(worker), now: now, lastMs: -1}
}

// Next returns the next ID. If the clock has moved backwards, or the
// sequence for the current millisecond is used up, Next waits until the
// clock passes the last issued ID instead of reusing the past.
func (g *Generator) Next() ID {
	g.mu.Lock()
	defer g.mu.Unlock()

	for {
		ms := g.millis()
		switch {
		case ms > g.lastMs:
			g.lastMs, g.sequence = ms, 0
		case ms == g.lastMs && g.sequence < maxSequence:
			g.sequence++
		default:
			// Clock rollback or sequence exhausted: wait for the clock.
			time.Sleep(max(time.Duration(g.lastMs-ms)*time.Millisecond, 100*time.Microsecond))
			continue
		}
		return ID(g.lastMs<<timeShift | g.worker<<sequenceBits | g.sequence)
	}
}

func (g *Generator) millis() int64 {
	t := g.now()
	ms := t.Sub(Epoch).Milliseconds()
	if ms < 0 || ms > maxMillis {
		panic(fmt.Sprintf("snowflake: clock %v is outside the ID range", t))
	}
	return ms
}
