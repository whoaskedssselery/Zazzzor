package users_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/users"
)

type selfBody struct {
	userBody
	Bio          string  `json:"bio"`
	Status       string  `json:"status"`
	CustomStatus *string `json:"custom_status"`
}

func TestUpdateAboutStatusAndLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.client()
	u := register(t, c, "alice", "password123")
	id := userID(t, u)
	patch := func(body any) *selfBody {
		t.Helper()
		resp := c.Do(http.MethodPatch, "/api/v1/users/@me", body)
		if resp.Status != http.StatusOK {
			t.Fatalf("PATCH %v = %d: %s", body, resp.Status, resp.Body)
		}
		var got selfBody
		resp.JSON(&got)
		return &got
	}

	got := patch(map[string]any{"bio": "  люблю котов\nи чай  ", "status": "dnd", "custom_status": "  на паре  "})
	if got.Bio != "люблю котов\nи чай" || got.Status != "dnd" || got.CustomStatus == nil || *got.CustomStatus != "на паре" {
		t.Fatalf("PATCH = %+v", got)
	}
	// Only the user hears of "about me"; the status goes to the gateway.
	if n := len(e.pub.peerUpdates()); n != 0 {
		t.Fatalf("peer USER_UPDATE = %d, want 0", n)
	}
	sets := e.pub.ofType(events.PresenceSet)
	if len(sets) != 1 {
		t.Fatalf("PRESENCE_SET events = %d, want 1", len(sets))
	}
	if p, ok := sets[0].Payload.(events.PresenceSetPayload); !ok || p.UserID != id || p.Status != "dnd" ||
		p.CustomStatus == nil || *p.CustomStatus != "на паре" {
		t.Fatalf("PRESENCE_SET = %#v", sets[0].Payload)
	}

	// null and a blank line both clear the status line; the rest stays.
	if got = patch(map[string]any{"custom_status": nil}); got.CustomStatus != nil || got.Status != "dnd" || got.Bio == "" {
		t.Fatalf("clear = %+v", got)
	}
	patch(map[string]any{"custom_status": "x"})
	if got = patch(map[string]any{"custom_status": "   "}); got.CustomStatus != nil {
		t.Fatalf("blank line = %+v", got)
	}
	// Writing the same values again announces nothing more.
	before := len(e.pub.ofType(events.PresenceSet))
	patch(map[string]any{"status": "dnd", "custom_status": nil})
	if after := len(e.pub.ofType(events.PresenceSet)); after != before {
		t.Fatalf("PRESENCE_SET %d -> %d for no change", before, after)
	}

	for _, tc := range []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"bio": strings.Repeat("я", 191)}, "bio"},
		{map[string]any{"bio": "a\x00b"}, "bio"},
		{map[string]any{"bio": nil}, "bio"},
		{map[string]any{"status": "away"}, "status"},
		{map[string]any{"status": nil}, "status"},
		{map[string]any{"custom_status": strings.Repeat("я", 129)}, "custom_status"},
		{map[string]any{"custom_status": "a\nb"}, "custom_status"},
		{map[string]any{"custom_status": 5}, "custom_status"},
	} {
		p := problemOf(t, c.Do(http.MethodPatch, "/api/v1/users/@me", tc.body), http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != tc.field {
			t.Fatalf("%v: errors = %+v", tc.body, p.Errors)
		}
	}
	// One request with several bad fields reports them all and writes none.
	p := problemOf(t, c.Do(http.MethodPatch, "/api/v1/users/@me",
		map[string]any{"display_name": " ", "status": "away", "bio": "ok"}), http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 2 {
		t.Fatalf("errors = %+v, want display_name and status", p.Errors)
	}
	if got := me(t, c); got.DisplayName != "alice" {
		t.Fatalf("display name changed by a refused patch: %+v", got)
	}
}

// links is a fake guilds.ProfileLink: who knows whom, with their guilds.
type links map[[2]snowflake.ID][]snowflake.ID

func (l links) ProfileLink(_ context.Context, viewer, target snowflake.ID) ([]snowflake.ID, bool, error) {
	m, ok := l[[2]snowflake.ID{viewer, target}]
	return m, ok, nil
}

func TestProfileVisibleToPeersOnly(t *testing.T) {
	t.Parallel()
	l := links{}
	e := newEnv(t, func(o *users.Options) { o.Peers = l })
	ca, cb, cc := e.client(), e.client(), e.client()
	a := userID(t, register(t, ca, "alice", "password123"))
	b := userID(t, register(t, cb, "bob", "password123"))
	register(t, cc, "carol", "password123")
	if resp := cb.Do(http.MethodPatch, "/api/v1/users/@me", map[string]any{"bio": "привет"}); resp.Status != http.StatusOK {
		t.Fatalf("PATCH bio = %d", resp.Status)
	}
	l[[2]snowflake.ID{a, b}] = []snowflake.ID{42}

	resp := ca.Do(http.MethodGet, "/api/v1/users/"+b.String()+"/profile", nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET profile = %d: %s", resp.Status, resp.Body)
	}
	var got struct {
		User           userBody `json:"user"`
		Bio            string   `json:"bio"`
		MutualGuildIDs []string `json:"mutual_guild_ids"`
	}
	resp.JSON(&got)
	if got.User.Username != "bob" || got.Bio != "привет" || len(got.MutualGuildIDs) != 1 || got.MutualGuildIDs[0] != "42" {
		t.Fatalf("profile = %+v", got)
	}

	// Strangers, unknown users and bad IDs all read as not found.
	problemOf(t, cc.Do(http.MethodGet, "/api/v1/users/"+b.String()+"/profile", nil), http.StatusNotFound, "not_found")
	problemOf(t, ca.Do(http.MethodGet, "/api/v1/users/123/profile", nil), http.StatusNotFound, "not_found")
	problemOf(t, ca.Do(http.MethodGet, "/api/v1/users/nope/profile", nil), http.StatusNotFound, "not_found")

	// Your own profile is always yours to see.
	if resp := cb.Do(http.MethodGet, "/api/v1/users/"+b.String()+"/profile", nil); resp.Status != http.StatusOK {
		t.Fatalf("own profile = %d", resp.Status)
	}
}
