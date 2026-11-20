package gateway_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships"
)

const relationshipPath = "/api/v1/users/@me/relationships"

func TestFriendGatewayWithoutSharedGuild(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{PresenceGrace: 50 * time.Millisecond})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	eve, c := e.user("eve")
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ecl, _ := connect(t, eve)
	if r := alice.Do(http.MethodPost, relationshipPath, map[string]string{"username": "BOB"}); r.Status != http.StatusNoContent {
		t.Fatalf("request status = %d, body = %s", r.Status, r.Body)
	}
	var added relationships.Relationship
	decode(t, bcl.waitFor(events.RelationshipAdd).D, &added)
	if added.ID != a.id(t) || added.Type != relationships.Incoming {
		t.Fatalf("incoming = %+v, want Alice", added)
	}
	if r := bob.Do(http.MethodPut, relationshipPath+"/"+a.ID, map[string]int{"type": 1}); r.Status != http.StatusNoContent {
		t.Fatalf("accept status = %d, body = %s", r.Status, r.Body)
	}
	for _, tc := range []struct {
		conn   *wsClient
		viewer userBody
		other  userBody
	}{{acl, a, b}, {bcl, b, a}} {
		frames := e.untilSentinel(tc.conn, tc.viewer)
		var accepted, online bool
		for _, frame := range frames {
			switch frame.T {
			case events.RelationshipAdd:
				var rel relationships.Relationship
				decode(t, frame.D, &rel)
				accepted = accepted || (rel.ID == tc.other.id(t) && rel.Type == relationships.Friend)
			case events.PresenceUpdate:
				var p gateway.Presence
				decode(t, frame.D, &p)
				online = online || (p.UserID == tc.other.id(t) && p.Status == gateway.StatusOnline)
			}
		}
		if !accepted || !online {
			t.Fatalf("friend and online dispatches to %s = %v", tc.viewer.ID, frames)
		}
	}
	if err := bcl.ws.CloseNow(); err != nil {
		t.Fatalf("close Bob: %v", err)
	}
	var p gateway.Presence
	decode(t, acl.waitFor(events.PresenceUpdate).D, &p)
	if p.UserID != b.id(t) || p.Status != gateway.StatusOffline {
		t.Fatalf("offline = %+v, want Bob offline", p)
	}
	newBob := dial(t, bob)
	var ready struct {
		Guilds        []gateway.GuildState
		Presences     []gateway.Presence
		Relationships []relationships.Relationship
	}
	decode(t, newBob.waitFor(gateway.EventReady).D, &ready)
	if len(ready.Guilds) != 0 || len(ready.Relationships) != 1 || ready.Relationships[0].ID != a.id(t) ||
		ready.Relationships[0].Type != relationships.Friend {
		t.Fatalf("READY = %+v, want Alice as friend with no shared guild", ready)
	}
	seenAlice := false
	for _, status := range ready.Presences {
		seenAlice = seenAlice || (status.UserID == a.id(t) && status.Status == gateway.StatusOnline)
	}
	if !seenAlice {
		t.Fatalf("READY presences = %+v, want Alice online", ready.Presences)
	}
	decode(t, acl.waitFor(events.PresenceUpdate).D, &p)
	if p.UserID != b.id(t) || p.Status != gateway.StatusOnline {
		t.Fatalf("online = %+v, want Bob online", p)
	}
	if r := bob.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "Bobby"}); r.Status != http.StatusOK {
		t.Fatalf("profile status = %d, body = %s", r.Status, r.Body)
	}
	var profile userBody
	decode(t, acl.waitFor(events.UserUpdate).D, &profile)
	if profile.ID != b.ID {
		t.Fatalf("profile = %+v, want Bob", profile)
	}
	if r := alice.Do(http.MethodDelete, relationshipPath+"/"+b.ID, nil); r.Status != http.StatusNoContent {
		t.Fatalf("remove status = %d, body = %s", r.Status, r.Body)
	}
	var gone relationships.Removed
	decode(t, acl.waitFor(events.RelationshipRemove).D, &gone)
	if gone.ID != b.id(t) || gone.Type != relationships.Friend {
		t.Fatalf("removal = %+v, want Bob", gone)
	}
	if r := bob.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "Robert"}); r.Status != http.StatusOK {
		t.Fatalf("profile status = %d, body = %s", r.Status, r.Body)
	}
	e.bus.Publish(t.Context(), events.Event{Type: events.PresenceUpdate, Audience: events.Audience{Kind: events.AudiencePeers, UserIDs: []snowflake.ID{b.id(t)}},
		ExcludeUserID: b.id(t), Payload: gateway.Presence{UserID: b.id(t), Status: gateway.StatusOffline}})
	for _, tc := range []struct {
		conn *wsClient
		user userBody
	}{{acl, a}, {ecl, c}} {
		for _, f := range e.untilSentinel(tc.conn, tc.user) {
			switch f.T {
			case events.PresenceUpdate, events.UserUpdate, events.RelationshipAdd, events.RelationshipRemove:
				t.Errorf("unexpected dispatch to %s: %s %s", tc.user.ID, f.T, f.D)
			}
		}
	}
}

func TestStaleFriendDispatchDoesNotSendPresence(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	acl, _ := connect(t, alice)
	_, _ = connect(t, bob)
	friend, err := e.m.Auth.GetUser(t.Context(), b.id(t))
	if err != nil {
		t.Fatalf("load Bob: %v", err)
	}
	// Reproduce a queued ADD consumed after the pair is gone.
	e.bus.Publish(t.Context(), events.Event{Type: events.RelationshipAdd,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{a.id(t)}},
		Payload:  relationships.Relationship{ID: friend.ID, User: friend.Public(), Type: relationships.Friend, Since: friend.Public().CreatedAt}})
	for _, frame := range e.untilSentinel(acl, a) {
		if frame.T == events.PresenceUpdate {
			t.Errorf("stale friendship disclosed presence: %s", frame.D)
		}
	}
}
