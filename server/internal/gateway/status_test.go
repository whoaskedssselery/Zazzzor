package gateway_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// presencesWithLines is presenceUpdates with the status line, if any.
func presencesWithLines(t *testing.T, fs []frame) []string {
	t.Helper()
	var out []string
	for _, f := range fs {
		if f.T != events.PresenceUpdate {
			continue
		}
		var p presenceBody
		decode(t, f.D, &p)
		s := p.UserID + ":" + p.Status
		if p.CustomStatus != nil {
			s += ":" + *p.CustomStatus
		}
		out = append(out, s)
	}
	return out
}

func setStatus(t *testing.T, c *testkit.Client, body map[string]any) {
	t.Helper()
	expect(t, c.Do(http.MethodPatch, "/api/v1/users/@me", body), http.StatusOK)
}

// TestChosenStatusReachesPeers: peers see the status and line the user
// chose; invisible looks offline to them, at once and in later READYs.
func TestChosenStatusReachesPeers(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	drain(e, map[*wsClient]userBody{acl: au})
	_ = bcl

	setStatus(t, bob, map[string]any{"status": "dnd", "custom_status": "на паре"})
	if got := presencesWithLines(t, e.untilSentinel(acl, au)); !slices.Equal(got, []string{bu.ID + ":dnd:на паре"}) {
		t.Fatalf("after dnd: %v", got)
	}
	// "About me" alone says nothing to peers.
	setStatus(t, bob, map[string]any{"bio": "привет"})
	if got := presencesWithLines(t, e.untilSentinel(acl, au)); len(got) != 0 {
		t.Fatalf("after bio: %v", got)
	}
	setStatus(t, bob, map[string]any{"status": "invisible"})
	if got := presencesWithLines(t, e.untilSentinel(acl, au)); !slices.Equal(got, []string{bu.ID + ":offline"}) {
		t.Fatalf("after invisible: %v", got)
	}
	// A line changed while invisible stays hidden.
	setStatus(t, bob, map[string]any{"custom_status": "тихо"})
	if got := presencesWithLines(t, e.untilSentinel(acl, au)); len(got) != 0 {
		t.Fatalf("line while invisible: %v", got)
	}
	_, r := connect(t, e.login("alice"))
	st, _ := r.guild(g.ID)
	if got := onlineIn(st); slices.Contains(got, bu.ID) {
		t.Fatalf("invisible bob in READY presences %v", got)
	}

	setStatus(t, bob, map[string]any{"status": "idle"})
	if got := presencesWithLines(t, e.untilSentinel(acl, au)); !slices.Equal(got, []string{bu.ID + ":idle:тихо"}) {
		t.Fatalf("after idle: %v", got)
	}
}

// TestInvisibleFromTheStart: a user who connects invisible never shows up.
func TestInvisibleFromTheStart(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, cu)
	setStatus(t, carol, map[string]any{"status": "invisible"})
	acl, _ := connect(t, alice)
	drain(e, map[*wsClient]userBody{acl: au})

	connect(t, carol)
	if got := presenceUpdates(t, e.untilSentinel(acl, au)); len(got) != 0 {
		t.Fatalf("invisible carol announced: %v", got)
	}
}
