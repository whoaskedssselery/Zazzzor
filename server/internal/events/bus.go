package events

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Bus fans events out to subscribers without ever blocking the publisher.
//
// Every subscriber owns a buffered channel. When it is full, the bus drops
// that subscriber: it delivers a final Overflow event, then closes the
// channel. One extra channel slot is reserved for the Overflow event, so it is
// always delivered; a reader sees buffered events, then Overflow, then a
// closed channel, and must treat both Overflow and a closed channel as "resync
// by reconnecting". Other subscribers are unaffected.
type Bus struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool

	published *prometheus.CounterVec
	dropped   prometheus.Counter
}

type subscriber struct {
	ch    chan Event
	limit int
}

// NewBus returns a bus. reg may be nil, in which case metrics are kept but
// not exported.
func NewBus(reg prometheus.Registerer) *Bus {
	b := &Bus{
		subs: make(map[*subscriber]struct{}),
		published: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "zazzzor_events_published_total",
			Help: "Events published on the in-process bus, by type.",
		}, []string{"type"}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "zazzzor_events_dropped_subscribers_total",
			Help: "Subscribers dropped because their buffer overflowed.",
		}),
	}
	if reg != nil {
		reg.MustRegister(b.published, b.dropped)
	}
	return b
}

// Subscribe registers a subscriber that can hold buffer undelivered events
// (minimum 1). The returned cancel func unsubscribes and closes the channel;
// it is idempotent and safe to call concurrently with Publish. Subscribing to
// a closed bus returns an already closed channel.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	buffer = max(buffer, 1)
	s := &subscriber{ch: make(chan Event, buffer+1), limit: buffer}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(s.ch)
		return s.ch, func() {}
	}
	b.subs[s] = struct{}{}
	return s.ch, func() { b.remove(s) }
}

func (b *Bus) remove(s *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
}

// Publish delivers e to every subscriber and returns immediately. It is a
// no-op after Close.
func (b *Bus) Publish(_ context.Context, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.published.WithLabelValues(e.Type).Inc()
	for s := range b.subs {
		// Only Publish (under b.mu) sends, so len can only shrink under us.
		if len(s.ch) >= s.limit {
			s.ch <- Event{Type: Overflow} // the reserved slot is always free
			close(s.ch)
			delete(b.subs, s)
			b.dropped.Inc()
			continue
		}
		s.ch <- e
	}
}

// Close closes every subscriber channel and turns later Publish calls into
// no-ops. It is idempotent.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for s := range b.subs {
		close(s.ch)
	}
	b.subs = nil
}
