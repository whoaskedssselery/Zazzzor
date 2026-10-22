package messages_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestCreateAndListMessages(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, general, _ := newGuild(t, alice, "home")

	m1 := post(t, alice, general.ID, "  first\n")
	if m1.Content != "first" || m1.ChannelID != general.ID || m1.GuildID != g.ID || m1.Author == nil ||
		m1.Author.ID != a.ID || m1.EditedAt != nil || m1.ReplyToID != nil || m1.ReferencedMessage != nil ||
		m1.Nonce != nil {
		t.Fatalf("created message = %+v", m1)
	}
	m2 := post(t, alice, general.ID, "second")
	m3 := post(t, alice, general.ID, "third")

	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{m3.ID, m2.ID, m1.ID}},
		{"?limit=2", []string{m3.ID, m2.ID}},
		{"?before=" + m3.ID, []string{m2.ID, m1.ID}},
		{"?before=" + m2.ID + "&limit=1", []string{m1.ID}},
		{"?before=" + m1.ID, []string{}},
		{"?after=" + m1.ID, []string{m2.ID, m3.ID}},
		{"?after=" + m1.ID + "&limit=1", []string{m2.ID}},
		{"?after=" + m3.ID, []string{}},
		{"?limit=100", []string{m3.ID, m2.ID, m1.ID}},
	}
	for _, tc := range cases {
		if got := ids(list(t, alice, general.ID, tc.query)); !slices.Equal(got, tc.want) {
			t.Errorf("list%s = %v, want %v", tc.query, got, tc.want)
		}
	}
	got := list(t, alice, general.ID, "?limit=1")[0]
	if got.Content != "third" || got.Author == nil || got.Author.Username != "alice" || got.CreatedAt == "" {
		t.Fatalf("listed message = %+v", got)
	}

	for _, q := range []string{"?limit=101", "?limit=0", "?limit=x", "?limit=", "?before=0123", "?before=x",
		"?after=-1", "?after=0", "?before=" + m2.ID + "&after=" + m1.ID} {
		problemOf(t, alice.Do(http.MethodGet, messagesPath(general.ID)+q, nil), http.StatusBadRequest, "validation_failed")
	}
}

func TestCreateMessageLengthCountsRunes(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	if m := post(t, alice, general.ID, strings.Repeat("😀", messages.MaxContent)); len([]rune(m.Content)) != 4000 {
		t.Fatalf("stored %d runes", len([]rune(m.Content)))
	}
	post(t, alice, general.ID, strings.Repeat("я", 4000))
	// Hebrew with an explicit bidi override: allowed in messages.
	post(t, alice, general.ID, "שלום "+string(rune(0x202E))+"abc")
	p := problemOf(t, send(alice, general.ID, map[string]any{"content": strings.Repeat("😀", 4001)}),
		http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "content" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	// Surrounding white space does not count.
	post(t, alice, general.ID, "  "+strings.Repeat("я", 4000)+"\n\t ")
}

func TestCreateMessageRejectsInvisibleOnly(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	for _, content := range []any{
		"  " + string(rune(0x200B)) + " \n",
		"",
		" \t\n",
		string([]rune{0x200B, 0x200D, 0xFEFF, 0x3000, 0x2029}),
		"a" + string(rune(0)) + "b",
		nil,
	} {
		body := map[string]any{}
		if content != nil {
			body["content"] = content
		}
		p := problemOf(t, send(alice, general.ID, body), http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "content" {
			t.Fatalf("content %q: errors = %+v", content, p.Errors)
		}
	}
	// A zero-width character next to a visible one is fine.
	post(t, alice, general.ID, "x"+string(rune(0x200B)))
}

func TestSendRequiresPermission(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	post(t, alice, general.ID, "hello")

	denyEveryone(t, alice, g, general.ID, permissions.SendMessages)
	problemOf(t, send(bobC, general.ID, map[string]any{"content": "hi"}), http.StatusForbidden, "missing_permission")
	problemOf(t, bobC.Do(http.MethodPost, "/api/v1/channels/"+general.ID+"/typing", nil), http.StatusForbidden, "missing_permission")
	if got := list(t, bobC, general.ID, ""); len(got) != 1 {
		t.Fatalf("bob lists %d messages", len(got))
	}
	// The owner is not bound by the overwrite.
	post(t, alice, general.ID, "still here")

	denyEveryone(t, alice, g, general.ID, permissions.ReadMessageHistory)
	problemOf(t, bobC.Do(http.MethodGet, messagesPath(general.ID), nil), http.StatusForbidden, "missing_permission")
	post(t, bobC, general.ID, "hi")
}

// TestHiddenChannelMessages404 is the messages part of
// channels.TestHiddenChannelIsNotFound: a channel the caller may not view,
// or of a server they are not in, does not exist for any message endpoint.
func TestHiddenChannelMessages404(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bobC, bob := e.user("bob")
	strangerC, _ := e.user("eve")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	secret := createChannel(t, alice, g, "secret")
	denyEveryone(t, alice, g, secret.ID, permissions.ViewChannel)
	m := post(t, alice, secret.ID, "classified")
	pub := post(t, alice, general.ID, "public")

	tries := func(c *testkit.Client, channelID, messageID string) {
		t.Helper()
		for _, resp := range []*testkit.Response{
			c.Do(http.MethodGet, messagesPath(channelID), nil),
			c.Do(http.MethodGet, messagesPath(channelID)+"?before="+messageID, nil),
			send(c, channelID, map[string]any{"content": "x"}),
			c.Do(http.MethodPatch, messagePath(channelID, messageID), map[string]any{"content": "x"}),
			c.Do(http.MethodDelete, messagePath(channelID, messageID), nil),
			c.Do(http.MethodPost, "/api/v1/channels/"+channelID+"/typing", nil),
		} {
			problemOf(t, resp, http.StatusNotFound, "not_found")
		}
	}
	tries(bobC, secret.ID, m.ID)
	tries(strangerC, general.ID, pub.ID)
	tries(strangerC, secret.ID, m.ID)
	tries(alice, "123", m.ID)
	tries(alice, "0"+general.ID, pub.ID)
	// The visible channel works for bob.
	expect(t, bobC.Do(http.MethodGet, messagesPath(general.ID), nil), http.StatusOK)

	// A malformed or foreign message ID in a visible channel is not found.
	for _, id := range []string{"0" + pub.ID, "x", m.ID, "123"} {
		problemOf(t, alice.Do(http.MethodPatch, messagePath(general.ID, id), map[string]any{"content": "x"}),
			http.StatusNotFound, "not_found")
		problemOf(t, alice.Do(http.MethodDelete, messagePath(general.ID, id), nil), http.StatusNotFound, "not_found")
	}
	if got := ids(list(t, alice, secret.ID, "")); !slices.Equal(got, []string{m.ID}) {
		t.Fatalf("secret channel holds %v", got)
	}
}

func TestEditOnlyAuthor(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	other := createChannel(t, alice, g, "other")
	m := post(t, bobC, general.ID, "original")

	// Not even the owner may edit someone else's message.
	problemOf(t, alice.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": "hijack"}),
		http.StatusForbidden, "missing_permission")
	problemOf(t, bobC.Do(http.MethodPatch, messagePath(other.ID, m.ID), map[string]any{"content": "moved"}),
		http.StatusNotFound, "not_found")
	problemOf(t, bobC.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": " " + string(rune(0x200B))}),
		http.StatusBadRequest, "validation_failed")
	problemOf(t, bobC.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{}),
		http.StatusBadRequest, "validation_failed")

	resp := bobC.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": " edited "})
	expect(t, resp, http.StatusOK)
	var edited messageBody
	resp.JSON(&edited)
	if edited.Content != "edited" || edited.EditedAt == nil || edited.ID != m.ID || edited.CreatedAt != m.CreatedAt {
		t.Fatalf("edited = %+v", edited)
	}
	if n := len(e.pub.ofType(events.MessageUpdate)); n != 1 {
		t.Fatalf("%d MESSAGE_UPDATE events, want 1", n)
	}

	// The same content again changes nothing.
	resp = bobC.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": "edited"})
	expect(t, resp, http.StatusOK)
	var same messageBody
	resp.JSON(&same)
	if same.Content != "edited" || same.EditedAt == nil || *same.EditedAt != *edited.EditedAt {
		t.Fatalf("no-op edit = %+v, want edited_at %s", same, *edited.EditedAt)
	}
	if n := len(e.pub.ofType(events.MessageUpdate)); n != 1 {
		t.Fatalf("%d MESSAGE_UPDATE events after a no-op, want 1", n)
	}
	if got := list(t, alice, general.ID, ""); len(got) != 1 || got[0].Content != "edited" || got[0].EditedAt == nil {
		t.Fatalf("listed = %+v", got)
	}
}

func TestDeleteByAuthorOrManageMessages(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bobC, bob := e.user("bob")
	carolC, carol := e.user("carol")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	e.join(g, carol)
	m1 := post(t, bobC, general.ID, "one")
	m2 := post(t, bobC, general.ID, "two")
	m3 := post(t, bobC, general.ID, "three")

	problemOf(t, carolC.Do(http.MethodDelete, messagePath(general.ID, m1.ID), nil), http.StatusForbidden, "missing_permission")
	expect(t, bobC.Do(http.MethodDelete, messagePath(general.ID, m1.ID), nil), http.StatusNoContent)
	problemOf(t, bobC.Do(http.MethodDelete, messagePath(general.ID, m1.ID), nil), http.StatusNotFound, "not_found")

	// The owner holds every permission.
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, m2.ID), nil), http.StatusNoContent)

	// So does a member given MANAGE_MESSAGES through a role.
	resp := alice.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles",
		map[string]any{"name": "mods", "permissions": perm(permissions.ManageMessages)})
	expect(t, resp, http.StatusCreated)
	var role struct {
		ID string `json:"id"`
	}
	resp.JSON(&role)
	expect(t, alice.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+carol.ID,
		map[string]any{"roles": []string{role.ID}}), http.StatusOK)
	expect(t, carolC.Do(http.MethodDelete, messagePath(general.ID, m3.ID), nil), http.StatusNoContent)

	if got := list(t, alice, general.ID, ""); len(got) != 0 {
		t.Fatalf("left %v", ids(got))
	}
	dels := e.pub.ofType(events.MessageDelete)
	if len(dels) != 3 {
		t.Fatalf("%d MESSAGE_DELETE events, want 3", len(dels))
	}
}

func TestReplyMustBeSameChannel(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, general, _ := newGuild(t, alice, "home")
	other := createChannel(t, alice, g, "other")
	orig := post(t, alice, general.ID, "question")

	for _, replyTo := range []string{orig.ID, "123"} {
		p := problemOf(t, send(alice, other.ID, map[string]any{"content": "answer", "reply_to_id": replyTo}),
			http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "reply_to_id" {
			t.Fatalf("errors = %+v", p.Errors)
		}
	}
	problemOf(t, send(alice, other.ID, map[string]any{"content": "answer", "reply_to_id": "x"}),
		http.StatusBadRequest, "invalid_body")

	resp := send(alice, general.ID, map[string]any{"content": "answer", "reply_to_id": orig.ID})
	expect(t, resp, http.StatusCreated)
	var reply messageBody
	resp.JSON(&reply)
	ref := reply.ReferencedMessage
	if reply.ReplyToID == nil || *reply.ReplyToID != orig.ID || ref == nil || ref.ID != orig.ID ||
		ref.Content != "question" || ref.Author == nil || ref.Author.ID != a.ID {
		t.Fatalf("reply = %+v (ref %+v)", reply, ref)
	}
	got := list(t, alice, general.ID, "")
	if len(got) != 2 || got[0].ReferencedMessage == nil || got[0].ReferencedMessage.ID != orig.ID ||
		got[1].ReferencedMessage != nil {
		t.Fatalf("listed = %+v", got)
	}
}

func TestReplyToDeletedMessageIsNull(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")
	orig := post(t, alice, general.ID, "question")
	resp := send(alice, general.ID, map[string]any{"content": "answer", "reply_to_id": orig.ID})
	expect(t, resp, http.StatusCreated)
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, orig.ID), nil), http.StatusNoContent)

	got := list(t, alice, general.ID, "")
	if len(got) != 1 || got[0].ReplyToID == nil || *got[0].ReplyToID != orig.ID || got[0].ReferencedMessage != nil {
		t.Fatalf("listed = %+v", got)
	}
}

func TestNonceEchoed(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	nonce := strings.Repeat("ж", messages.MaxNonce)
	resp := send(alice, general.ID, map[string]any{"content": "hi", "nonce": nonce})
	expect(t, resp, http.StatusCreated)
	var m messageBody
	resp.JSON(&m)
	if m.Nonce == nil || *m.Nonce != nonce {
		t.Fatalf("nonce = %v", m.Nonce)
	}
	created := e.pub.ofType(events.MessageCreate)
	if len(created) != 1 {
		t.Fatalf("%d MESSAGE_CREATE events", len(created))
	}
	if got := payloadOf(t, created[0])["nonce"]; got != nonce {
		t.Fatalf("event nonce = %v", got)
	}
	// It is not stored.
	if got := list(t, alice, general.ID, ""); got[0].Nonce != nil {
		t.Fatalf("listed nonce = %v", *got[0].Nonce)
	}
	p := problemOf(t, send(alice, general.ID, map[string]any{"content": "hi", "nonce": nonce + "ж"}),
		http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "nonce" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	// Without a nonce the event carries none.
	post(t, alice, general.ID, "plain")
	if _, ok := payloadOf(t, e.pub.ofType(events.MessageCreate)[1])["nonce"]; ok {
		t.Fatal("event without a nonce carries one")
	}
}

func TestMessageRateLimit(t *testing.T) {
	e := newEnv(t, func(o *messages.Options) {
		o.SendLimiter = ratelimit.New(rate.Every(time.Hour), 2, 2*time.Hour)
		o.WriteLimiter = ratelimit.New(rate.Every(time.Hour), 2, 2*time.Hour)
	})
	alice, _ := e.user("alice")
	bobC, bob := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)

	m1 := post(t, alice, general.ID, "1")
	m2 := post(t, alice, general.ID, "2")
	resp := send(alice, general.ID, map[string]any{"content": "3"})
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// Per user: bob still may.
	post(t, bobC, general.ID, "bob")

	// Edits and deletions share one limit, apart from sending.
	expect(t, alice.Do(http.MethodPatch, messagePath(general.ID, m1.ID), map[string]any{"content": "1!"}), http.StatusOK)
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, m1.ID), nil), http.StatusNoContent)
	problemOf(t, alice.Do(http.MethodDelete, messagePath(general.ID, m2.ID), nil), http.StatusTooManyRequests, "rate_limited")
	problemOf(t, alice.Do(http.MethodPatch, messagePath(general.ID, m2.ID), map[string]any{"content": "2!"}),
		http.StatusTooManyRequests, "rate_limited")
}

func TestTypingThrottled(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	bobC, bob := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bob)
	other := createChannel(t, alice, g, "other")
	typing := func(c *testkit.Client, channelID string) {
		t.Helper()
		expect(t, c.Do(http.MethodPost, "/api/v1/channels/"+channelID+"/typing", nil), http.StatusNoContent)
	}

	typing(bobC, general.ID)
	typing(bobC, general.ID)
	got := e.pub.ofType(events.TypingStart)
	if len(got) != 1 {
		t.Fatalf("%d TYPING_START events after two calls, want 1", len(got))
	}
	ev := got[0]
	if ev.Audience.Kind != events.AudienceChannel || ev.Audience.ChannelID.String() != general.ID ||
		ev.Audience.GuildID.String() != g.ID || ev.ExcludeUserID.String() != bob.ID {
		t.Fatalf("event = %+v", ev)
	}
	p := payloadOf(t, ev)
	if p["channel_id"] != general.ID || p["guild_id"] != g.ID || p["user_id"] != bob.ID || p["timestamp"] == "" {
		t.Fatalf("payload = %v", p)
	}
	if _, err := time.Parse(time.RFC3339, p["timestamp"].(string)); err != nil {
		t.Fatalf("timestamp: %v", err)
	}

	// The throttle is per user and channel.
	typing(bobC, other.ID)
	typing(alice, general.ID)
	got = e.pub.ofType(events.TypingStart)
	if len(got) != 3 || got[2].ExcludeUserID.String() != a.ID {
		t.Fatalf("events = %+v", got)
	}
}

func TestVoiceChannelRejectsMessages(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	g, _, voice := newGuild(t, alice, "home")
	var category channelBody
	for _, c := range listChannels(t, alice, g) {
		if c.Type == 4 {
			category = c
		}
	}
	for _, id := range []string{voice.ID, category.ID} {
		problemOf(t, send(alice, id, map[string]any{"content": "hi"}), http.StatusBadRequest, "not_a_text_channel")
		problemOf(t, alice.Do(http.MethodPost, "/api/v1/channels/"+id+"/typing", nil), http.StatusBadRequest, "not_a_text_channel")
	}
	if n := len(e.pub.ofType(events.MessageCreate)); n != 0 {
		t.Fatalf("%d MESSAGE_CREATE events", n)
	}
}

func TestMessageEventsPublished(t *testing.T) {
	bus := events.NewBus(nil)
	t.Cleanup(bus.Close)
	sub, cancel := bus.Subscribe(64)
	t.Cleanup(cancel)
	e := newEnv(t, func(o *messages.Options) { o.Publisher = bus })
	alice, a := e.user("alice")
	g, general, _ := newGuild(t, alice, "home")

	m := post(t, alice, general.ID, "hello")
	expect(t, alice.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": "hello!"}), http.StatusOK)
	expect(t, alice.Do(http.MethodDelete, messagePath(general.ID, m.ID), nil), http.StatusNoContent)

	var got []events.Event
	timeout := time.After(5 * time.Second)
	for len(got) < 3 {
		select {
		case ev := <-sub:
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("got only %+v", got)
		}
	}
	for i, typ := range []string{events.MessageCreate, events.MessageUpdate, events.MessageDelete} {
		ev := got[i]
		if ev.Type != typ || ev.Audience.Kind != events.AudienceChannel ||
			ev.Audience.ChannelID.String() != general.ID || ev.Audience.GuildID.String() != g.ID || ev.ExcludeUserID != 0 {
			t.Fatalf("event %d = %+v, want %s to the channel", i, ev, typ)
		}
	}
	created, updated, deleted := payloadOf(t, got[0]), payloadOf(t, got[1]), payloadOf(t, got[2])
	author, _ := created["author"].(map[string]any)
	if created["id"] != m.ID || created["content"] != "hello" || author["id"] != a.ID || created["guild_id"] != g.ID {
		t.Fatalf("MESSAGE_CREATE payload = %v", created)
	}
	if updated["id"] != m.ID || updated["content"] != "hello!" || updated["edited_at"] == nil {
		t.Fatalf("MESSAGE_UPDATE payload = %v", updated)
	}
	want := map[string]any{"id": m.ID, "channel_id": general.ID, "guild_id": g.ID}
	if len(deleted) != len(want) || deleted["id"] != want["id"] || deleted["channel_id"] != want["channel_id"] ||
		deleted["guild_id"] != want["guild_id"] {
		t.Fatalf("MESSAGE_DELETE payload = %v, want %v", deleted, want)
	}
}

// payloadOf is the event payload as the gateway will send it.
func payloadOf(t *testing.T, ev events.Event) map[string]any {
	t.Helper()
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
