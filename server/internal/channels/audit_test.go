package channels_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

// auditLog remembers the entries recorded.
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

func (l *auditLog) actions() []audit.Action {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]audit.Action, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.Action
	}
	return out
}

func TestChannelChangesAreAudited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	g := createGuild(t, owner, "log")
	role := createRole(t, owner, g, "muted", 0)
	e.log.mu.Lock()
	e.log.entries = nil
	e.log.mu.Unlock()

	ch := createChannel(t, owner, g, map[string]any{"name": "news", "type": 0})
	expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID, map[string]any{"topic": "only news"}), http.StatusOK)
	expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID, map[string]any{"topic": "only news"}), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, role, 0, 0, permissions.SendMessages), http.StatusOK)
	expect(t, putOverwrite(owner, ch.ID, role, 0, 0, permissions.SendMessages|permissions.AddReactions),
		http.StatusOK)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID+"/permissions/"+role, nil), http.StatusNoContent)
	expect(t, owner.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID, nil), http.StatusNoContent)

	// The second PATCH changed nothing and records nothing.
	want := []audit.Action{audit.ChannelCreate, audit.ChannelUpdate, audit.OverwriteCreate, audit.OverwriteUpdate,
		audit.OverwriteDelete, audit.ChannelDelete}
	if got := e.log.actions(); !slices.Equal(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	upd := e.log.entries[1]
	if len(upd.Changes) != 1 || upd.Changes[0].Key != "topic" || upd.TargetID != ch.id(t) {
		t.Errorf("update = %+v", upd)
	}
	ow := e.log.entries[3]
	if ow.Options["id"] != role || ow.Options["type"] != "0" || ow.TargetID != ch.id(t) {
		t.Errorf("overwrite update = %+v", ow)
	}
}
