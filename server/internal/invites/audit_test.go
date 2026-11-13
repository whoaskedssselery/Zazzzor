package invites_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites"
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

func TestInvitesAreAudited(t *testing.T) {
	log := &auditLog{}
	e := newEnv(t, func(o *invites.Options) { o.Audit = log })
	owner, _ := e.user("owner")
	_, ch := newGuild(t, owner, "home")
	inv := createInvite(t, owner, ch.ID, map[string]any{"max_uses": 5})
	expect(t, owner.Do(http.MethodDelete, "/api/v1/invites/"+inv.Code, nil), http.StatusNoContent)

	log.mu.Lock()
	defer log.mu.Unlock()
	var acts []audit.Action
	for _, en := range log.entries {
		acts = append(acts, en.Action)
		if en.Options["code"] != inv.Code || en.Options["channel_id"] != ch.ID {
			t.Errorf("entry options = %v", en.Options)
		}
	}
	if !slices.Equal(acts, []audit.Action{audit.InviteCreate, audit.InviteDelete}) {
		t.Fatalf("actions = %v", acts)
	}
}
