package messages_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

type auditLog struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (l *auditLog) Record(_ context.Context, _ audit.Tx, e audit.Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	return nil
}

func TestModeratorDeleteIsAudited(t *testing.T) {
	t.Parallel()
	log := &auditLog{}
	e := newEnv(t, func(o *messages.Options) { o.Audit = log })
	alice, au := e.user("alice")
	bobC, bob := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	own := post(t, bobC, general.ID, "mine")
	other := post(t, bobC, general.ID, "rude")

	expect(t, bobC.Do(http.MethodDelete, messagePath(general.ID, own.ID), nil), http.StatusNoContent)
	if len(log.entries) != 0 {
		t.Fatalf("an author deleting their own message was audited: %+v", log.entries)
	}
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, other.ID), nil), http.StatusNoContent)
	if len(log.entries) != 1 {
		t.Fatalf("entries = %+v", log.entries)
	}
	got := log.entries[0]
	if got.Action != audit.MessageDelete || got.ActorID.String() != au.ID || got.TargetID.String() != bob.ID ||
		got.Options["channel_id"] != general.ID {
		t.Errorf("entry = %+v", got)
	}
}
