package events_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
)

func ev(t string) events.Event { return events.Event{Type: t} }

func recv(t *testing.T, ch <-chan events.Event) (events.Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return events.Event{}, false
	}
}

func TestBusDeliversToAllSubscribers(t *testing.T) {
	b := events.NewBus(nil)
	defer b.Close()
	a, cancelA := b.Subscribe(4)
	c, cancelC := b.Subscribe(4)
	defer cancelA()
	defer cancelC()

	b.Publish(context.Background(), ev(events.MessageCreate))
	b.Publish(context.Background(), ev(events.MessageDelete))

	for _, ch := range []<-chan events.Event{a, c} {
		if e, _ := recv(t, ch); e.Type != events.MessageCreate {
			t.Fatalf("first = %q", e.Type)
		}
		if e, _ := recv(t, ch); e.Type != events.MessageDelete {
			t.Fatalf("second = %q", e.Type)
		}
	}
}

func TestBusSlowSubscriberDropped(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := events.NewBus(reg)
	defer b.Close()
	slow, _ := b.Subscribe(1)
	fast, cancel := b.Subscribe(8)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for _, typ := range []string{events.MessageCreate, events.MessageUpdate, events.MessageDelete} {
			b.Publish(context.Background(), ev(typ))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	for _, want := range []string{events.MessageCreate, events.MessageUpdate, events.MessageDelete} {
		if e, _ := recv(t, fast); e.Type != want {
			t.Fatalf("fast got %q, want %q", e.Type, want)
		}
	}

	if e, _ := recv(t, slow); e.Type != events.MessageCreate {
		t.Fatalf("slow first = %q", e.Type)
	}
	if e, _ := recv(t, slow); e.Type != events.Overflow {
		t.Fatalf("slow second = %q, want Overflow", e.Type)
	}
	if _, ok := recv(t, slow); ok {
		t.Fatal("slow channel not closed after Overflow")
	}

	if got := testutil.ToFloat64(droppedMetric(t, reg)); got != 1 {
		t.Fatalf("dropped = %v", got)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := events.NewBus(nil)
	defer b.Close()
	ch, cancel := b.Subscribe(4)

	cancel()
	cancel() // idempotent
	b.Publish(context.Background(), ev(events.MessageCreate))

	if _, ok := recv(t, ch); ok {
		t.Fatal("channel still open after cancel")
	}
}

func TestBusCloseClosesSubscribersAndIgnoresPublish(t *testing.T) {
	b := events.NewBus(nil)
	ch, cancel := b.Subscribe(4)

	b.Close()
	b.Close()
	b.Publish(context.Background(), ev(events.MessageCreate)) // must not panic
	cancel()                                                  // must not panic

	if _, ok := recv(t, ch); ok {
		t.Fatal("channel open after Close")
	}
	late, _ := b.Subscribe(1)
	if _, ok := recv(t, late); ok {
		t.Fatal("subscribe after Close returned an open channel")
	}
}

func TestBusConcurrentPublishAndCancel(*testing.T) {
	b := events.NewBus(nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 200 {
				b.Publish(context.Background(), ev(events.TypingStart))
			}
		}()
		go func() {
			defer wg.Done()
			ch, cancel := b.Subscribe(2)
			for range 3 {
				select {
				case <-ch:
				default:
				}
			}
			cancel()
		}()
	}
	wg.Wait()
	b.Close()
}

func TestBusCountsPublishedByType(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := events.NewBus(reg)
	defer b.Close()
	b.Publish(context.Background(), ev(events.GuildCreate))
	b.Publish(context.Background(), ev(events.GuildCreate))

	n, err := testutil.GatherAndCount(reg, "zazzzor_events_published_total")
	if err != nil || n != 1 {
		t.Fatalf("series = %d, err = %v", n, err)
	}
}

// droppedMetric returns the dropped-subscribers counter as a collector.
func droppedMetric(t *testing.T, reg *prometheus.Registry) prometheus.Collector {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "zazzzor_events_dropped_subscribers_total" {
			c := prometheus.NewCounter(prometheus.CounterOpts{Name: "tmp"})
			c.Add(mf.GetMetric()[0].GetCounter().GetValue())
			return c
		}
	}
	t.Fatal("dropped metric not found")
	return nil
}
