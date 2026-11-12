package gateway_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestReplyEventsRespectHistoryAccess(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, channel := newGuild(t, alice, "guild")
	e.join(g, bu)
	original := post(t, alice, channel.ID, "original")
	overwrite(t, alice, channel.ID, bu.ID, 1, 0, permissions.ReadMessageHistory)
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)

	path := "/api/v1/channels/" + channel.ID + "/messages"
	resp := alice.Do(http.MethodPost, path, map[string]string{"content": "answer", "reply_to_id": original.ID})
	expect(t, resp, http.StatusCreated)
	var reply messageBody
	resp.JSON(&reply)
	check := func(event string) {
		t.Helper()
		for _, recipient := range []struct {
			name string
			data frame
			want bool
		}{{"alice", acl.waitFor(event), true}, {"bob", bcl.waitFor(event), false}} {
			var m struct {
				ID                string `json:"id"`
				ReplyToID         string `json:"reply_to_id"`
				ReferencedMessage any    `json:"referenced_message"`
			}
			decode(t, recipient.data.D, &m)
			if m.ID != reply.ID || m.ReplyToID != original.ID || (m.ReferencedMessage != nil) != recipient.want {
				t.Errorf("%s %s: id = %s, link = %v, reference present = %v, want %v", recipient.name,
					event, m.ID, m.ReplyToID, m.ReferencedMessage != nil, recipient.want)
			}
		}
	}
	check(events.MessageCreate)
	expect(t, alice.Do(http.MethodPatch, path+"/"+reply.ID, map[string]string{"content": "edited"}), http.StatusOK)
	check(events.MessageUpdate)
}
