package messages_test

import (
	"context"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
)

func TestDeleteDoesNotWaitForAttachmentTrash(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, user := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	msg := post(t, c, ch.ID, "delete me")
	blocker, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin trash blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(t.Context(), "LOCK TABLE attachment_trash IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock trash: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- e.msgs.Delete(t.Context(), parseID(t, ch.ID), parseID(t, msg.ID), parseID(t, user.ID))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = blocker.Rollback(context.Background())
		if deleteErr := <-done; deleteErr != nil {
			t.Errorf("Delete after releasing trash: %v", deleteErr)
		}
		t.Fatal("Delete waited for the locked trash queue, want immediate publication")
	}
	if got := len(e.pub.ofType(events.MessageDelete)); got != 1 {
		t.Errorf("delete events = %d, want 1", got)
	}
}

func TestAttachmentTrashSweepIsBounded(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.pool.Exec(t.Context(), "INSERT INTO attachment_trash (id, channel_id) SELECT id, 123 FROM generate_series(1, 150) AS id"); err != nil {
		t.Fatalf("queue trash: %v", err)
	}
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments: %v", err)
	}
	var remaining int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM attachment_trash").Scan(&remaining); err != nil {
		t.Fatalf("count trash: %v", err)
	}
	if remaining == 0 || remaining == 150 {
		t.Errorf("remaining trash = %d, want a partially processed queue", remaining)
	}
}

func TestFailedTrashBatchDoesNotStarveLaterEntries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO attachment_trash (id, channel_id)
		SELECT id, CASE WHEN id <= 100 THEN -1 ELSE 123 END FROM generate_series(1, 101) AS id`); err != nil {
		t.Fatalf("queue trash: %v", err)
	}
	// Invalid references fail persistently, but later valid entries must progress.
	for range 2 {
		_ = e.msgs.SweepAttachments(t.Context())
	}
	var remaining int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM attachment_trash WHERE id = 101").Scan(&remaining); err != nil {
		t.Fatalf("count healthy entry: %v", err)
	}
	if remaining != 0 {
		t.Errorf("healthy queued entries = %d, want 0", remaining)
	}
}
