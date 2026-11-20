package gateway_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const (
	dmCreated = "DM_CHANNEL_CREATE"
	dmUpdated = "DM_CHANNEL_UPDATE"
	dmDeleted = "DM_CHANNEL_DELETE"
)

func sendDM(t *testing.T, c *testkit.Client, path string, body any) messages.PublicMessage {
	t.Helper()
	r := c.Do(http.MethodPost, path+"/messages", body)
	dmExpect(t, r, http.StatusCreated)
	var m messages.PublicMessage
	r.JSON(&m)
	if m.GuildID != nil {
		t.Fatalf("DM guild_id = %v, want null", m.GuildID)
	}
	return m
}

func readDMReady(t *testing.T, c *testkit.Client, id string, want int32) {
	t.Helper()
	client := dial(t, c)
	defer func() { _ = client.ws.CloseNow() }()
	var ready struct {
		Private    []privateChannelBody `json:"private_channels"`
		ReadStates []messages.ReadState `json:"read_states"`
	}
	decode(t, client.waitFor(gateway.EventReady).D, &ready)
	if len(ready.Private) != 1 || ready.Private[0].ID != id {
		t.Fatalf("READY private_channels = %+v, want %s", ready.Private, id)
	}
	for _, state := range ready.ReadStates {
		if state.ChannelID.String() == id {
			if state.MentionCount != want {
				t.Errorf("DM unread = %d, want %d", state.MentionCount, want)
			}
			return
		}
	}
	t.Fatalf("READY read_states = %+v, want DM %s", ready.ReadStates, id)
}

func TestDirectChannelGatewayAndUnread(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	makeFriends(t, alice, bob, a, b)
	bc, _ := connect(t, bob)
	ch := openDM(t, alice, b.ID, http.StatusCreated)
	bc.waitFor(dmCreated)
	path := "/api/v1/channels/" + ch.ID
	dmExpect(t, bob.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	bc.waitFor(dmDeleted)

	first := sendDM(t, alice, path, map[string]string{"content": "first"})
	var reopened privateChannelBody
	decode(t, bc.waitFor(dmCreated).D, &reopened)
	if reopened.ID != ch.ID || reopened.LastMessageID == nil || *reopened.LastMessageID != first.ID.String() {
		t.Fatalf("reopened DM = %+v, want latest message", reopened)
	}
	var received messages.PublicMessage
	decode(t, bc.waitFor(events.MessageCreate).D, &received)
	if received.ID != first.ID || received.GuildID != nil {
		t.Fatalf("delivery = %+v", received)
	}
	second := sendDM(t, alice, path, map[string]string{"content": "second"})
	readDMReady(t, bob, ch.ID, 2)
	dmExpect(t, bob.Do(http.MethodPost, path+"/messages/"+second.ID.String()+"/ack", nil), http.StatusNoContent)
	var ack messages.ReadState
	decode(t, bc.waitFor(events.MessageAck).D, &ack)
	if ack.ChannelID.String() != ch.ID || ack.MentionCount != 0 || ack.LastReadID != second.ID {
		t.Fatalf("ack = %+v, want acknowledged second message", ack)
	}
	sendDM(t, alice, path, map[string]string{"content": "third"})
	reply := sendDM(t, bob, path, map[string]string{"content": "@everyone @here <@" + a.ID + ">", "reply_to_id": first.ID.String()})
	if reply.ReferencedMessage == nil || reply.ReferencedMessage.ID != first.ID ||
		reply.MentionEveryone || reply.MentionHere || len(reply.MentionUserIDs) != 1 || reply.MentionUserIDs[0] != a.id(t) {
		t.Fatalf("reply = %+v, want direct mention and reference", reply)
	}
	readDMReady(t, bob, ch.ID, 0)
	dmExpect(t, alice.Do(http.MethodPost, path+"/typing", nil), http.StatusNoContent)
	var typing messages.TypingPayload
	decode(t, bc.waitFor(events.TypingStart).D, &typing)
	if typing.GuildID != nil || typing.UserID != a.id(t) {
		t.Fatalf("typing = %+v", typing)
	}
	target := path + "/messages/" + first.ID.String()
	dmExpect(t, bob.Do(http.MethodPut, target+"/reactions/thumb:999/@me", nil), http.StatusBadRequest)
	dmExpect(t, bob.Do(http.MethodPut, target+"/reactions/%F0%9F%91%8D/@me", nil), http.StatusNoContent)
	var reaction messages.ReactionPayload
	decode(t, bc.waitFor(events.MessageReactionAdd).D, &reaction)
	if reaction.GuildID != nil || reaction.Emoji.ID != nil {
		t.Fatalf("reaction = %+v", reaction)
	}
	dmExpect(t, bob.Do(http.MethodPatch, target, map[string]string{"content": "not mine"}), http.StatusForbidden)
	dmExpect(t, bob.Do(http.MethodDelete, target, nil), http.StatusForbidden)
	dmExpect(t, alice.Do(http.MethodPatch, target, map[string]string{"content": "edited"}), http.StatusOK)
	dmExpect(t, alice.Do(http.MethodDelete, target, nil), http.StatusNoContent)
	var removed messages.DeletePayload
	decode(t, bc.waitFor(events.MessageDelete).D, &removed)
	if removed.GuildID != nil || removed.ID != first.ID {
		t.Fatalf("message deletion = %+v", removed)
	}
}

func TestRemovedGroupRecipientStopsReceivingMessages(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	carol, c := e.user("carol")
	makeFriends(t, alice, bob, a, b)
	makeFriends(t, alice, carol, a, c)
	cc, _ := connect(t, carol)
	bc, _ := connect(t, bob)
	group := createDMGroup(t, alice, b.ID, c.ID)
	path := "/api/v1/channels/" + group.ID
	cc.waitFor(dmCreated)
	bc.waitFor(dmCreated)
	dmExpect(t, alice.Do(http.MethodDelete, path+"/recipients/"+c.ID, nil), http.StatusNoContent)
	cc.waitFor(dmDeleted)
	var updated privateChannelBody
	decode(t, bc.waitFor(dmUpdated).D, &updated)
	if len(updated.Recipients) != 1 || updated.Recipients[0].ID != a.ID {
		t.Fatalf("updated group = %+v, want Alice as Bob's only peer", updated)
	}
	// A block ends the friendship and leaves group membership intact.
	dmExpect(t, alice.Do(http.MethodPut, "/api/v1/users/@me/relationships/"+b.ID, map[string]int{"type": 2}), http.StatusNoContent)
	sent := sendDM(t, bob, path, map[string]string{"content": "still in group"})
	var received messages.PublicMessage
	decode(t, bc.waitFor(events.MessageCreate).D, &received)
	if received.ID != sent.ID {
		t.Fatalf("received = %+v, want %s", received, sent.ID)
	}
	dmExpect(t, carol.Do(http.MethodGet, path+"/messages", nil), http.StatusNotFound)
	for _, f := range e.untilSentinel(cc, c) {
		switch f.T {
		case dmCreated, dmUpdated, events.MessageCreate, events.MessageUpdate, events.MessageAck:
			t.Errorf("removed recipient received %s: %s", f.T, f.D)
		}
	}
}
