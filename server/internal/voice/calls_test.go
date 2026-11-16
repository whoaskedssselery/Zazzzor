package voice_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// calls returns the CALL_* events so far.
func (p *recorder) calls() []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []events.Event
	for _, e := range p.events {
		switch e.Type {
		case events.CallCreate, events.CallUpdate, events.CallDelete:
			out = append(out, e)
		}
	}
	return out
}

// lastCall returns the newest CALL_* event, failing without one.
func (e *env) lastCall() events.Event {
	e.t.Helper()
	list := e.pub.calls()
	if len(list) == 0 {
		e.t.Fatal("no CALL_* event")
	}
	return list[len(list)-1]
}

func ringing(ev events.Event) []snowflake.ID {
	if c, ok := ev.Payload.(voice.Call); ok {
		return c.Ringing
	}
	return nil
}

// dmPair makes alice and bob, who share a server, and their 1:1 DM.
func dmPair(t *testing.T, e *env) (alice *testkit.Client, a userBody, bob *testkit.Client, b userBody, dm string) {
	t.Helper()
	alice, a = e.user("alice")
	bob, b = e.user("bob")
	g, _, _ := newGuild(t, alice, "home")
	e.join(g, b)
	ch, _, err := e.chans.OpenDirect(t.Context(), a.id(t), b.id(t))
	if err != nil {
		t.Fatalf("OpenDirect: %v", err)
	}
	return alice, a, bob, b, ch.ID.String()
}

func TestCallRingsTheOtherAndEndsWithItsLastParticipant(t *testing.T) {
	e := newEnv(t)
	alice, a, bob, b, dm := dmPair(t, e)
	dmID := parseID(t, dm)

	// Everyone in a DM may talk, share and film.
	grants := claims(t, joinVoice(t, alice, dm).Token).Video
	if !grants.GetCanPublish() || !grants.GetCanSubscribe() {
		t.Fatalf("grants = %+v, want publish and subscribe", grants)
	}
	ev := e.lastCall()
	if ev.Type != events.CallCreate || !slices.Equal(ringing(ev), []snowflake.ID{b.id(t)}) {
		t.Fatalf("after alice's token: %s %v, want CALL_CREATE ringing bob", ev.Type, ringing(ev))
	}
	if ev.Audience.Kind != events.AudienceChannel || ev.Audience.ChannelID != dmID {
		t.Fatalf("audience = %+v, want the DM's recipients", ev.Audience)
	}

	e.connect(alice, a, dm, "RM_dm", "PA_alice")
	e.connect(bob, b, dm, "RM_dm", "PA_bob")
	if ev = e.lastCall(); ev.Type != events.CallUpdate || len(ringing(ev)) != 0 {
		t.Fatalf("after bob joined: %s %v, want CALL_UPDATE ringing nobody", ev.Type, ringing(ev))
	}
	updates := e.pub.updates()
	if last := updates[len(updates)-1]; last.GuildID != nil || last.ChannelID != dmID {
		t.Fatalf("voice state = %+v, want guild_id null in the DM", last.PublicState)
	}
	calls, states := e.voice.PrivateCalls([]snowflake.ID{dmID})
	if len(calls) != 1 || len(states) != 2 {
		t.Fatalf("PrivateCalls = %v, %v; want the call and both", calls, states)
	}

	expect(t, alice.Do(http.MethodDelete, "/api/v1/users/@me/voice", nil), http.StatusNoContent)
	if ev = e.lastCall(); ev.Type == events.CallDelete {
		t.Fatal("the call ended while bob is still in it")
	}
	expect(t, bob.Do(http.MethodDelete, "/api/v1/users/@me/voice", nil), http.StatusNoContent)
	if ev = e.lastCall(); ev.Type != events.CallDelete {
		t.Fatalf("after both left: %s, want CALL_DELETE", ev.Type)
	}
}

func TestDecliningACallNobodyIsInEndsIt(t *testing.T) {
	e := newEnv(t)
	alice, _, bob, _, dm := dmPair(t, e)
	joinVoice(t, alice, dm) // a token, not yet connected
	expect(t, bob.Do(http.MethodPost, "/api/v1/channels/"+dm+"/call/decline", nil), http.StatusNoContent)
	if ev := e.lastCall(); ev.Type != events.CallDelete {
		t.Fatalf("after the decline: %s, want CALL_DELETE", ev.Type)
	}
	// Declining again, or with no call, changes nothing.
	n := len(e.pub.calls())
	expect(t, bob.Do(http.MethodPost, "/api/v1/channels/"+dm+"/call/decline", nil), http.StatusNoContent)
	if len(e.pub.calls()) != n {
		t.Fatal("a second decline announced something")
	}
}

func TestDeclineNeedsAPrivateChannelOfTheCaller(t *testing.T) {
	e := newEnv(t)
	_, _, _, _, dm := dmPair(t, e)
	carol, _ := e.user("carol")
	problemOf(t, carol.Do(http.MethodPost, "/api/v1/channels/"+dm+"/call/decline", nil), http.StatusNotFound, "not_found")
	_, _, vc := newGuild(t, carol, "other")
	problemOf(t, carol.Do(http.MethodPost, "/api/v1/channels/"+vc.ID+"/call/decline", nil), http.StatusBadRequest,
		"not_a_voice_channel")
}

func TestAnUnansweredCallStopsRinging(t *testing.T) {
	e := newEnv(t, func(o *voice.Options) { o.RingFor = 300 * time.Millisecond })
	alice, a, _, _, dm := dmPair(t, e)
	e.connect(alice, a, dm, "RM_dm", "PA_alice")
	// On a slow machine the ringing can run out before alice's webhook
	// arrives: the call then ends and starts again, ringing nobody, so the
	// last event is a create rather than an update. Either way nobody rings.
	eventually(t, "the ringing to stop", func() bool {
		list := e.pub.calls()
		last := list[len(list)-1]
		return last.Type != events.CallDelete && len(ringing(last)) == 0
	})
	if calls, _ := e.voice.PrivateCalls([]snowflake.ID{parseID(t, dm)}); len(calls) != 1 {
		t.Fatal("the call ended though alice is still in it")
	}
}

func TestABlockRefusesTheCall(t *testing.T) {
	e := newEnv(t)
	alice, a, _, b, dm := dmPair(t, e)
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO relationships(user_id,other_id,type,since) VALUES ($1,$2,2,now())`,
		int64(b.id(t)), int64(a.id(t))); err != nil {
		t.Fatalf("block: %v", err)
	}
	problemOf(t, alice.Do(http.MethodPost, joinPath(dm), nil), http.StatusForbidden, "cannot_message_user")
	if len(e.pub.calls()) != 0 {
		t.Fatal("a refused call rang")
	}
}

func TestLeavingAGroupLeavesItsCall(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	carol, c := e.user("carol")
	if _, err := e.pool.Exec(t.Context(), `
INSERT INTO relationships(user_id,other_id,type,since)
SELECT x, y, 1, now() FROM (VALUES ($1::bigint,$2::bigint),($2,$1),($1,$3::bigint),($3,$1)) AS p(x,y)`,
		int64(a.id(t)), int64(b.id(t)), int64(c.id(t))); err != nil {
		t.Fatalf("friends: %v", err)
	}
	group, err := e.chans.CreateGroup(t.Context(), a.id(t), []snowflake.ID{b.id(t), c.id(t)})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	id := group.ID.String()
	joinVoice(t, alice, id)
	if ev := e.lastCall(); len(ringing(ev)) != 2 {
		t.Fatalf("a group call rings %v, want bob and carol", ringing(ev))
	}
	e.connect(alice, a, id, "RM_g", "PA_alice")
	e.connect(bob, b, id, "RM_g", "PA_bob")
	e.connect(carol, c, id, "RM_g", "PA_carol")
	if err = e.chans.RemoveGroupRecipient(t.Context(), group.ID, a.id(t), c.id(t)); err != nil {
		t.Fatalf("remove carol: %v", err)
	}
	e.voice.RecheckPrivate(t.Context(), group.ID, c.id(t))
	_, states := e.voice.PrivateCalls([]snowflake.ID{group.ID})
	if len(states) != 2 || slices.ContainsFunc(states, func(s voice.State) bool { return s.UserID == c.id(t) }) {
		t.Fatalf("states = %v, want alice and bob only", states)
	}
	// Bob stays: he is still in the group.
	e.voice.RecheckPrivate(t.Context(), group.ID, b.id(t))
	if _, states = e.voice.PrivateCalls([]snowflake.ID{group.ID}); len(states) != 2 {
		t.Fatal("a recipient still in the group was taken out")
	}
}

// inCall reports whether userID is in the call of channelID.
func (e *env) inCall(channelID, userID snowflake.ID) bool {
	_, states := e.voice.PrivateCalls([]snowflake.ID{channelID})
	return slices.ContainsFunc(states, func(s voice.State) bool { return s.UserID == userID })
}

func TestABlockDuringACallEndsItAndRefusesOlderTokens(t *testing.T) {
	e := newEnv(t)
	alice, a, bob, b, dm := dmPair(t, e)
	dmID := parseID(t, dm)
	e.connect(alice, a, dm, "RM_dm", "PA_alice")
	// Bob holds a token from before the block and has not connected yet.
	join := claims(t, joinVoice(t, bob, dm).Token).Attributes["zazzzor.join"]
	if !e.inCall(dmID, a.id(t)) {
		t.Fatal("alice is not in the call")
	}

	if _, err := e.pool.Exec(t.Context(), `INSERT INTO relationships(user_id,other_id,type,since) VALUES ($1,$2,2,now())`,
		int64(b.id(t)), int64(a.id(t))); err != nil {
		t.Fatalf("block: %v", err)
	}
	e.voice.RecheckCalls(t.Context(), []snowflake.ID{b.id(t), a.id(t)})
	if e.inCall(dmID, a.id(t)) {
		t.Fatal("the block left alice in the 1:1 call")
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(dm, "RM_dm"),
		Participant: lkParticipant(b, "PA_bob", join)})
	if e.inCall(dmID, b.id(t)) {
		t.Fatal("a token from before the block joined the call")
	}
}

func TestADisabledAccountLeavesVoiceAndItsTokenStopsWorking(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	e.connect(alice, a, vc.ID, "RM_vc", "PA_alice")
	e.connect(bob, b, vc.ID, "RM_vc", "PA_bob")
	oldJoin := claims(t, joinVoice(t, bob, vc.ID).Token).Attributes["zazzzor.join"]
	if _, ok := e.stateOf(parseID(t, g.ID), b.id(t)); !ok {
		t.Fatal("bob is not in voice")
	}

	if _, err := e.auth.SetDisabled(t.Context(), b.id(t), true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	if _, ok := e.stateOf(parseID(t, g.ID), b.id(t)); ok {
		t.Fatal("a disabled account stayed in voice")
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(vc.ID, "RM_vc"),
		Participant: lkParticipant(b, "PA_bob2", oldJoin)})
	if _, ok := e.stateOf(parseID(t, g.ID), b.id(t)); ok {
		t.Fatal("a disabled account joined with a token issued before")
	}
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); !ok {
		t.Fatal("alice was taken out too")
	}
}

func TestAGroupCallDoesNotRingWhoBlockedTheCaller(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	_, b := e.user("bob")
	_, c := e.user("carol")
	if _, err := e.pool.Exec(t.Context(), `
INSERT INTO relationships(user_id,other_id,type,since)
SELECT x, y, 1, now() FROM (VALUES ($1::bigint,$2::bigint),($2,$1),($1,$3::bigint),($3,$1)) AS p(x,y)`,
		int64(a.id(t)), int64(b.id(t)), int64(c.id(t))); err != nil {
		t.Fatalf("friends: %v", err)
	}
	group, err := e.chans.CreateGroup(t.Context(), a.id(t), []snowflake.ID{b.id(t), c.id(t)})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err = e.pool.Exec(t.Context(), `UPDATE relationships SET type = 2 WHERE user_id = $1 AND other_id = $2`,
		int64(b.id(t)), int64(a.id(t))); err != nil {
		t.Fatalf("block: %v", err)
	}
	joinVoice(t, alice, group.ID.String())
	if got := ringing(e.lastCall()); !slices.Equal(got, []snowflake.ID{c.id(t)}) {
		t.Fatalf("ringing = %v, want carol only", got)
	}
}
