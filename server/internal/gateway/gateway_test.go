package gateway_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestGatewayRequiresSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	upgrade := []testkit.RequestOption{
		testkit.WithHeader("Connection", "Upgrade"), testkit.WithHeader("Upgrade", "websocket"),
		testkit.WithHeader("Sec-WebSocket-Version", "13"), testkit.WithHeader("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ=="),
	}
	code := func(resp *testkit.Response) string {
		var p struct{ Code string }
		resp.JSON(&p)
		return p.Code
	}

	anon := testkit.NewClient(t, e.h)
	resp := anon.Do(http.MethodGet, "/api/v1/gateway", nil, upgrade...)
	if resp.Status != http.StatusUnauthorized || code(resp) != "unauthenticated" {
		t.Fatalf("without a session = %d %s", resp.Status, resp.Body)
	}
	if _, status, err := dialRaw(t, anon, testkit.DefaultOrigin); err == nil || status != http.StatusUnauthorized {
		t.Fatalf("websocket dial without a session: %v, %v", status, err)
	}

	alice, _ := e.user("alice")
	for _, opt := range []testkit.RequestOption{testkit.WithOrigin("https://evil.example"), testkit.WithoutOrigin()} {
		// With and without the upgrade headers: both checks say 403.
		for _, opts := range [][]testkit.RequestOption{append(slices.Clone(upgrade), opt), {opt}} {
			resp = alice.Do(http.MethodGet, "/api/v1/gateway", nil, opts...)
			if resp.Status != http.StatusForbidden || code(resp) != "origin_not_allowed" {
				t.Fatalf("foreign origin = %d %s", resp.Status, resp.Body)
			}
		}
	}
	if _, status, err := dialRaw(t, alice, "https://evil.example"); err == nil || status != http.StatusForbidden {
		t.Fatalf("websocket dial from a foreign origin: %v, %v", status, err)
	}
	// A plain GET with a session is not an upgrade.
	resp = alice.Do(http.MethodGet, "/api/v1/gateway", nil)
	if resp.Status != http.StatusUpgradeRequired || code(resp) != "upgrade_required" {
		t.Fatalf("plain GET = %d %s", resp.Status, resp.Body)
	}
	if n := e.metric("zazzzor_gateway_connections", ""); n != 0 {
		t.Fatalf("connections = %v after refusals", n)
	}
}

func TestGatewayHelloReady(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, general := newGuild(t, alice, "guild")
	secret := hiddenChannel(t, alice, g, "secret")
	e.join(g, bu)
	// Bob's own guild, where alice is not.
	g2, _ := newGuild(t, bob, "bobs")

	acl := dial(t, alice) // alice online first
	var aliceReady readyBody
	{
		f := acl.next()
		decode(t, f.D, &aliceReady)
	}
	if st, ok := aliceReady.guild(g.ID); !ok || !hasChannel(st.Channels, secret.ID) {
		t.Fatalf("owner READY lacks the hidden channel: %+v", aliceReady)
	}

	_, r := connect(t, bob)
	if r.User.ID != bu.ID || len(r.Guilds) != 2 {
		t.Fatalf("READY user/guilds = %+v", r)
	}
	st, ok := r.guild(g.ID)
	if !ok {
		t.Fatalf("READY lacks the guild: %+v", r)
	}
	if !hasChannel(st.Channels, general.ID) || hasChannel(st.Channels, secret.ID) {
		t.Fatalf("READY channels = %+v, want general and not secret", st.Channels)
	}
	if st.MyMember.User.ID != bu.ID || len(st.Members) != 2 {
		t.Fatalf("READY my_member/members = %+v / %+v", st.MyMember, st.Members)
	}
	if _, ok := r.guild(g2.ID); !ok {
		t.Fatal("READY lacks bob's own guild")
	}
	online := map[string]string{}
	for _, p := range r.Presences {
		online[p.UserID] = p.Status
	}
	if online[au.ID] != gateway.StatusOnline || online[bu.ID] != gateway.StatusOnline || len(online) != 2 {
		t.Fatalf("READY presences = %+v, want alice and bob online", r.Presences)
	}
}

func TestJoinSendsOwnGuildState(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general := newGuild(t, alice, "guild")
	secret := hiddenChannel(t, alice, g, "secret")
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)

	e.join(g, bu)
	var st guildStateBody
	decode(t, bcl.waitFor(events.GuildCreate).D, &st)
	if st.Guild.ID != g.ID || st.MyMember.User.ID != bu.ID ||
		!hasChannel(st.Channels, general.ID) || hasChannel(st.Channels, secret.ID) {
		t.Fatalf("GUILD_CREATE = %+v", st)
	}
	var add struct {
		Member memberBody `json:"member"`
	}
	decode(t, acl.waitFor(events.GuildMemberAdd).D, &add)
	if add.Member.User.ID != bu.ID {
		t.Fatalf("GUILD_MEMBER_ADD = %+v", add)
	}
}

func TestHeartbeatAck(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	cl, _ := connect(t, alice)
	for range 3 {
		cl.send(gateway.OpHeartbeat)
		f := cl.next()
		if f.Op != gateway.OpHeartbeatAck || f.hasS || f.T != "" {
			t.Fatalf("answer = %+v, want HEARTBEAT_ACK without s", f)
		}
	}
}

func TestThrottledHeartbeatsStayConnected(t *testing.T) {
	t.Parallel()
	const scale = 100
	timeout := gateway.DefaultHeartbeatTimeout / scale
	const throttledInterval = 60 * time.Second / scale
	e := newEnv(t, gateway.Options{HeartbeatTimeout: timeout})
	alice, _ := e.user("alice")
	cl, _ := connect(t, alice)
	start := time.Now()
	for range 4 {
		time.Sleep(throttledInterval)
		cl.send(gateway.OpHeartbeat)
		if f := cl.next(); f.Op != gateway.OpHeartbeatAck {
			t.Fatalf("frame = %+v, want HEARTBEAT_ACK", f)
		}
	}
	if time.Since(start) <= 150*time.Second/scale {
		t.Fatal("connection was not exercised past the scaled 150s timeout")
	}
	if n := e.metric("zazzzor_gateway_closes_total", "4009"); n != 0 {
		t.Fatalf("timeout closes = %v, want 0", n)
	}
}

func TestHeartbeatTimeoutCloses(t *testing.T) {
	t.Parallel()
	// Heartbeats come every quarter of the timeout: a busy test machine may
	// delay one by a lot without the connection dropping.
	const timeout = 2 * time.Second
	e := newEnv(t, gateway.Options{HeartbeatTimeout: timeout})
	alice, _ := e.user("alice")
	cl, _ := connect(t, alice)
	// Heartbeats keep it open past the timeout...
	for range 6 {
		time.Sleep(timeout / 4)
		cl.send(gateway.OpHeartbeat)
		if f := cl.next(); f.Op != gateway.OpHeartbeatAck {
			t.Fatalf("frame = %+v, want HEARTBEAT_ACK", f)
		}
	}
	// ...silence closes it.
	start := time.Now()
	if code := cl.closeStatus(); code != gateway.CloseSessionTimeout {
		t.Fatalf("close code = %d, want 4009", code)
	}
	if d := time.Since(start); d < timeout/2 {
		t.Fatalf("closed after %v, before the timeout", d)
	}
	eventually(t, "close metric", func() bool { return e.metric("zazzzor_gateway_closes_total", "4009") == 1 })
}

func TestUnknownOpCloses4002(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	for name, msg := range map[string]struct {
		typ websocket.MessageType
		b   string
	}{
		"unknown op":   {websocket.MessageText, `{"op":2,"d":null}`},
		"server op":    {websocket.MessageText, `{"op":11,"d":null}`},
		"no op":        {websocket.MessageText, `{"d":null}`},
		"invalid json": {websocket.MessageText, `{"op":1`},
		"binary":       {websocket.MessageBinary, `{"op":1,"d":null}`},
		"too big":      {websocket.MessageText, `{"op":1,"d":"` + strings.Repeat("x", gateway.MaxFrameBytes) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			cl, _ := connect(t, alice)
			cl.sendRaw(msg.typ, []byte(msg.b))
			if code := cl.closeStatus(); code != gateway.CloseDecodeError {
				t.Fatalf("close code = %d, want 4002", code)
			}
		})
	}
	// Exactly the limit is fine.
	cl, _ := connect(t, alice)
	pad := gateway.MaxFrameBytes - len(`{"op":1,"d":""}`)
	cl.sendRaw(websocket.MessageText, []byte(`{"op":1,"d":"`+strings.Repeat("x", pad)+`"}`))
	if f := cl.next(); f.Op != gateway.OpHeartbeatAck {
		t.Fatalf("frame of exactly %d bytes: %+v", gateway.MaxFrameBytes, f)
	}
}

func TestMessageCreateReachesOnlyViewers(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, general := newGuild(t, alice, "guild")
	e.join(g, bu)
	newGuild(t, carol, "elsewhere")
	// Hidden from bob only.
	secret := createChannel(t, alice, g, "secret")
	overwrite(t, alice, secret.ID, bu.ID, 1, 0, permissions.ViewChannel)

	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)

	hidden := post(t, alice, secret.ID, "for alice only")
	open := post(t, alice, general.ID, "for the guild")

	gotA := e.untilSentinel(acl, au)
	gotB := e.untilSentinel(bcl, bu)
	gotC := e.untilSentinel(ccl, cu)
	ids := func(fs []frame) []string {
		var out []string
		for _, f := range fs {
			if f.T == events.MessageCreate {
				var m messageBody
				decode(t, f.D, &m)
				out = append(out, m.ID)
			}
		}
		return out
	}
	if got := ids(gotA); !slices.Equal(got, []string{hidden.ID, open.ID}) {
		t.Fatalf("alice got %v, want both messages", got)
	}
	if got := ids(gotB); !slices.Equal(got, []string{open.ID}) {
		t.Fatalf("bob got %v, want only the visible message", got)
	}
	if got := ids(gotC); len(got) != 0 {
		t.Fatalf("carol (not a member) got %v", got)
	}
}

func TestHiddenChannelEventsStayHidden(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	e.join(g, cu)
	secret := hiddenChannel(t, alice, g, "secret")
	overwrite(t, alice, secret.ID, cu.ID, 1, permissions.ViewChannel, 0) // carol may view

	bcl, _ := connect(t, bob)
	ccl, _ := connect(t, carol)
	m := post(t, alice, secret.ID, "hidden")
	expect(t, alice.Do(http.MethodPost, "/api/v1/channels/"+secret.ID+"/typing", nil), http.StatusNoContent)
	expect(t, alice.Do(http.MethodPatch, "/api/v1/channels/"+secret.ID+"/messages/"+m.ID,
		map[string]string{"content": "edited"}), http.StatusOK)
	expect(t, alice.Do(http.MethodDelete, "/api/v1/channels/"+secret.ID+"/messages/"+m.ID, nil), http.StatusNoContent)

	want := []string{events.MessageCreate, events.TypingStart, events.MessageUpdate, events.MessageDelete}
	if got := types(e.untilSentinel(ccl, cu)); !slices.Equal(got, want) {
		t.Fatalf("viewer got %v, want %v", got, want)
	}
	for _, f := range e.untilSentinel(bcl, bu) {
		if strings.HasPrefix(f.T, "MESSAGE_") || f.T == events.TypingStart {
			t.Fatalf("non-viewer got %s: %s", f.T, f.D)
		}
	}
}

func TestGatewayLosesChannelAfterRoleChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	secret := hiddenChannel(t, alice, g, "secret")
	resp := alice.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]any{"name": "insiders", "permissions": "0"})
	expect(t, resp, http.StatusCreated)
	var role struct{ ID string }
	resp.JSON(&role)
	overwrite(t, alice, secret.ID, role.ID, 0, permissions.ViewChannel, 0)
	setRoles := func(roles ...string) {
		t.Helper()
		expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+bu.ID,
			map[string]any{"roles": append([]string{}, roles...)}), http.StatusOK)
	}
	setRoles(role.ID)

	acl, _ := connect(t, alice)
	bcl, r := connect(t, bob)
	if st, _ := r.guild(g.ID); !hasChannel(st.Channels, secret.ID) {
		t.Fatal("bob does not see the channel through his role")
	}
	post(t, alice, secret.ID, "before")
	bcl.waitFor(events.MessageCreate)

	setRoles() // the role is taken away
	var st guildStateBody
	decode(t, bcl.waitFor(events.GuildSync).D, &st)
	if st.Guild.ID != g.ID || hasChannel(st.Channels, secret.ID) {
		t.Fatalf("GUILD_SYNC after losing the role still lists the channel: %+v", st.Channels)
	}
	after := post(t, alice, secret.ID, "after")
	for _, f := range e.untilSentinel(bcl, bu) {
		if f.T == events.MessageCreate {
			t.Fatalf("bob got a message of a channel he cannot see: %s", f.D)
		}
	}
	var m messageBody
	decode(t, acl.waitFor(events.MessageCreate).D, &m) // "before"
	decode(t, acl.waitFor(events.MessageCreate).D, &m)
	if m.ID != after.ID {
		t.Fatalf("alice got %s, want the second message", m.ID)
	}
	_ = au
}

func TestDeleteChannelNotifiesAndHides(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general := newGuild(t, alice, "guild")
	e.join(g, bu)
	bcl, _ := connect(t, bob)

	expect(t, alice.Do(http.MethodDelete, "/api/v1/channels/"+general.ID, nil), http.StatusNoContent)
	var ch channelBody
	decode(t, bcl.waitFor(events.ChannelDelete).D, &ch)
	if ch.ID != general.ID {
		t.Fatalf("CHANNEL_DELETE id = %s, want %s", ch.ID, general.ID)
	}
	expect(t, bob.Do(http.MethodGet, "/api/v1/channels/"+general.ID, nil), http.StatusNotFound)

	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID, nil), http.StatusNoContent)
	var gd struct{ ID string }
	decode(t, bcl.waitFor(events.GuildDelete).D, &gd)
	if gd.ID != g.ID {
		t.Fatalf("GUILD_DELETE id = %s, want %s", gd.ID, g.ID)
	}
	expect(t, bob.Do(http.MethodGet, "/api/v1/guilds/"+g.ID, nil), http.StatusNotFound)
}

func TestLeaveGuildStopsEvents(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general := newGuild(t, alice, "guild")
	e.join(g, bu)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)

	expect(t, bob.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)
	var rm struct {
		User userBody `json:"user"`
	}
	decode(t, acl.waitFor(events.GuildMemberRemove).D, &rm)
	if rm.User.ID != bu.ID {
		t.Fatalf("GUILD_MEMBER_REMOVE = %+v", rm)
	}
	// After leaving, the guild's traffic must not reach bob.
	post(t, alice, general.ID, "after bob left")
	expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID, map[string]string{"name": "renamed"}), http.StatusOK)
	createChannel(t, alice, g, "new")

	got := e.untilSentinel(bcl, bu)
	deletes := 0
	for _, f := range got {
		switch f.T {
		case events.GuildDelete:
			var d struct{ ID string }
			decode(t, f.D, &d)
			if d.ID != g.ID {
				t.Fatalf("GUILD_DELETE of %s", d.ID)
			}
			deletes++
		case events.PresenceUpdate:
		default:
			t.Fatalf("bob got %s after leaving: %s", f.T, f.D)
		}
	}
	if deletes != 1 {
		t.Fatalf("bob got %d GUILD_DELETE, want exactly 1 (%v)", deletes, types(got))
	}
}

func TestPresenceMultipleConnections(t *testing.T) {
	t.Parallel()
	const grace = 300 * time.Millisecond
	e := newEnv(t, gateway.Options{PresenceGrace: grace})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	peers := []snowflake.ID{bu.id(t), au.id(t)}
	if got := e.gw.OnlineAmong(peers); len(got) != 0 {
		t.Fatalf("online before connecting = %v, want none", got)
	}
	presences := func(fs []frame) []presenceBody {
		var out []presenceBody
		for _, f := range fs {
			if f.T == events.PresenceUpdate {
				var p presenceBody
				decode(t, f.D, &p)
				out = append(out, p)
			}
		}
		return out
	}

	bcl, _ := connect(t, bob)
	tab1, r := connect(t, alice)
	if got := e.gw.OnlineAmong(peers); !slices.Equal(got, peers) {
		t.Fatalf("online after both connect = %v, want %v", got, peers)
	}
	if !slices.ContainsFunc(r.Presences, func(p presenceBody) bool { return p.UserID == bu.ID }) {
		t.Fatalf("alice's READY lacks bob's presence: %+v", r.Presences)
	}
	var p presenceBody
	decode(t, bcl.waitFor(events.PresenceUpdate).D, &p)
	if p.UserID != au.ID || p.Status != gateway.StatusOnline {
		t.Fatalf("first connection: %+v, want alice online", p)
	}
	tab2, _ := connect(t, alice)
	_ = tab1.ws.Close(websocket.StatusNormalClosure, "")
	eventually(t, "tab 1 to go", func() bool { return e.metric("zazzzor_gateway_connections", "") == 2 })
	time.Sleep(3 * grace)
	if got := e.gw.OnlineAmong(peers); !slices.Equal(got, peers) {
		t.Fatalf("online after one tab closes = %v, want %v", got, peers)
	}
	if got := presences(e.untilSentinel(bcl, bu)); len(got) != 0 {
		t.Fatalf("with a second tab open bob got %+v", got)
	}

	closed := time.Now()
	_ = tab2.ws.Close(websocket.StatusNormalClosure, "")
	decode(t, bcl.waitFor(events.PresenceUpdate).D, &p)
	if p.UserID != au.ID || p.Status != gateway.StatusOffline {
		t.Fatalf("after the last tab: %+v, want alice offline", p)
	}
	if got := e.gw.OnlineAmong(peers); !slices.Equal(got, peers[:1]) {
		t.Fatalf("online after Alice's offline event = %v, want %v", got, peers[:1])
	}
	if d := time.Since(closed); d < grace {
		t.Fatalf("offline after %v, before the grace period of %v", d, grace)
	}
	// Alice herself never hears about her own presence.
	_ = alice
}

func TestPresenceReconnectWithinGrace(t *testing.T) {
	t.Parallel()
	const grace = 2 * time.Second
	e := newEnv(t, gateway.Options{PresenceGrace: grace})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, bu)
	bcl, _ := connect(t, bob)
	tab, _ := connect(t, alice)
	bcl.waitFor(events.PresenceUpdate) // online

	_ = tab.ws.Close(websocket.StatusNormalClosure, "")
	eventually(t, "the tab to go", func() bool { return e.metric("zazzzor_gateway_connections", "") == 1 })
	connect(t, alice)
	time.Sleep(grace + grace/2)
	for _, f := range e.untilSentinel(bcl, bu) {
		if f.T == events.PresenceUpdate {
			t.Fatalf("a reconnection within the grace period announced %s", f.D)
		}
	}
	_ = au
}

func TestLogoutClosesSockets(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	other := e.login("alice") // a second session
	first, _ := connect(t, alice)
	second, _ := connect(t, other)

	expect(t, alice.Do(http.MethodPost, "/api/v1/auth/logout", nil), http.StatusNoContent)
	if code := first.closeStatus(); code != gateway.CloseAuthFailed {
		t.Fatalf("close code = %d, want 4004", code)
	}
	// The other session's socket stays.
	second.send(gateway.OpHeartbeat)
	if f := second.next(); f.Op != gateway.OpHeartbeatAck {
		t.Fatalf("other session: %+v", f)
	}
	// A revoked cookie cannot open a new one.
	if _, status, err := dialRaw(t, alice, testkit.DefaultOrigin); err == nil || status != http.StatusUnauthorized {
		t.Fatalf("dial after logout: %v, %v", status, err)
	}
}

func TestSequenceNumbersIncrease(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, au := e.user("alice")
	cl, _ := connect(t, alice) // READY was s=1
	want := int64(2)
	for range 3 {
		e.sentinel(au)
		cl.send(gateway.OpHeartbeat)
		for range 2 {
			f := cl.next()
			switch f.Op {
			case gateway.OpDispatch:
				if f.S != want {
					t.Fatalf("s = %d, want %d", f.S, want)
				}
				want++
			case gateway.OpHeartbeatAck:
				if f.hasS {
					t.Fatalf("HEARTBEAT_ACK carries s: %+v", f)
				}
			}
		}
	}
	if want != 5 {
		t.Fatalf("saw dispatches up to s=%d", want-1)
	}
}

func TestSlowConsumerClosed4008(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{SendBuffer: 8})
	alice, au := e.user("alice")
	bob, bu := e.user("bob")
	// Alice connects but never reads.
	if _, _, err := dialRaw(t, alice, testkit.DefaultOrigin); err != nil {
		t.Fatal(err)
	}
	fast, _ := connect(t, bob)
	eventually(t, "both connections", func() bool { return e.metric("zazzzor_gateway_connections", "") == 2 })

	big := map[string]string{"filler": strings.Repeat("x", 64<<10)}
	for range 400 {
		e.bus.Publish(t.Context(), events.Event{Type: events.UserUpdate,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{au.id(t)}}, Payload: big})
	}
	eventually(t, "the slow consumer to be closed with 4008", func() bool {
		return e.metric("zazzzor_gateway_closes_total", "4008") == 1
	})
	eventually(t, "the slow connection to go", func() bool { return e.metric("zazzzor_gateway_connections", "") == 1 })
	// The reader is unaffected.
	e.untilSentinel(fast, bu)
}

func TestBusOverflowClosesAllAndRecovers(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{BusBuffer: 1})
	alice, au := e.user("alice")
	g, _ := newGuild(t, alice, "guild")
	guild, err := e.m.Guilds.Lookup(t.Context(), parseID(t, g.ID))
	if err != nil {
		t.Fatal(err)
	}
	cl, _ := connect(t, alice)
	// Guild events cost the dispatcher a query each: a burst overflows a
	// one-event subscription.
	for range 200 {
		e.bus.Publish(t.Context(), events.Event{Type: events.GuildUpdate,
			Audience: events.Audience{Kind: events.AudienceGuild, GuildID: parseID(t, g.ID)},
			Payload:  guild.Public()})
	}
	if code := cl.closeStatus(); code != gateway.CloseOverloaded {
		t.Fatalf("close code = %d, want 4008", code)
	}
	// The gateway subscribed again: a new connection works.
	again, _ := connect(t, alice)
	e.untilSentinel(again, au)
}

func TestCloseSendsServiceRestart(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, _ := e.user("bob")
	a, _ := connect(t, alice)
	b, _ := connect(t, bob)
	g, _ := newGuild(t, alice, "guild")
	guild, err := e.m.Guilds.Lookup(t.Context(), parseID(t, g.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Events still being resolved when Close cancels their queries are
	// not dispatch errors (checked when the env is cleaned up).
	for range 50 {
		e.bus.Publish(t.Context(), events.Event{Type: events.GuildUpdate,
			Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guild.ID}, Payload: guild.Public()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	start := time.Now()
	if err = e.gw.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Close took %v", d)
	}
	for _, cl := range []*wsClient{a, b} {
		if code := cl.closeStatus(); code != gateway.CloseServiceRestart {
			t.Fatalf("close code = %d, want 1012", code)
		}
	}
	// Upgrades after Close are closed at once.
	ws, _, err := dialRaw(t, alice, testkit.DefaultOrigin)
	if err != nil {
		t.Fatal(err)
	}
	rctx, rcancel := context.WithTimeout(context.Background(), wait)
	defer rcancel()
	if _, _, err = ws.Read(rctx); websocket.CloseStatus(err) != gateway.CloseServiceRestart {
		t.Fatalf("new connection after Close: %v", err)
	}
	if n := e.metric("zazzzor_gateway_connections", ""); n != 0 {
		t.Fatalf("connections = %v after Close", n)
	}
}
