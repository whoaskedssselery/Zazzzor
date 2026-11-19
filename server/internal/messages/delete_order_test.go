package messages_test

import (
	"context"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

// delayedDelete holds the delete event after commit but before delivery.
type delayedDelete struct {
	recorder
	entered chan struct{}
	release chan struct{}
}

func (p *delayedDelete) Publish(ctx context.Context, event events.Event) {
	if event.Type == events.MessageDelete {
		close(p.entered)
		<-p.release
	}
	p.recorder.Publish(ctx, event)
}

func TestDelayedDeleteCannotFollowNewerCreate(t *testing.T) {
	p := &delayedDelete{entered: make(chan struct{}), release: make(chan struct{})}
	e := newEnv(t, func(o *messages.Options) { o.Publisher = p })
	c, user := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	old := post(t, c, ch.ID, "old")
	channel, author, id := parseID(t, ch.ID), parseID(t, user.ID), parseID(t, old.ID)
	deleted := make(chan error, 1)
	go func() { deleted <- e.msgs.Delete(t.Context(), channel, id, author) }()
	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		close(p.release)
		t.Fatal("delete did not reach publication")
	}
	created := make(chan error, 1)
	go func() {
		_, err := e.msgs.Create(t.Context(), channel, author, messages.CreateFields{Content: "new"})
		created <- err
	}()
	var createErr error
	completed := false
	select {
	case createErr = <-created:
		completed = true
	case <-time.After(200 * time.Millisecond):
	}
	close(p.release)
	if err := <-deleted; err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !completed {
		createErr = <-created
	}
	if createErr != nil {
		t.Fatalf("Create: %v", createErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var delivery []string
	for _, event := range p.events {
		if event.Type == events.MessageCreate || event.Type == events.MessageDelete {
			delivery = append(delivery, event.Type)
		}
	}
	if len(delivery) != 3 || delivery[1] != events.MessageDelete || delivery[2] != events.MessageCreate {
		t.Errorf("delivery = %v, want CREATE DELETE CREATE", delivery)
	}
}
