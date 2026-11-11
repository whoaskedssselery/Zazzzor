package gateway_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestReactionsReachOnlyChannelViewers(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	owner, ou := e.user("owner")
	member, mu := e.user("member")
	g, open := newGuild(t, owner, "Reactions")
	e.join(g, mu)
	hidden := createChannel(t, owner, g, "hidden")
	overwrite(t, owner, hidden.ID, mu.ID, 1, 0, permissions.ViewChannel)

	ocl, _ := connect(t, owner)
	mcl, _ := connect(t, member)
	privateMessage := post(t, owner, hidden.ID, "private")
	publicMessage := post(t, owner, open.ID, "public")
	for _, m := range []messageBody{privateMessage, publicMessage} {
		p := "/api/v1/channels/" + m.ChannelID + "/messages/" + m.ID + "/reactions/" + url.PathEscape("👍") + "/@me"
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			resp := owner.Do(method, p, nil)
			if resp.Status != http.StatusNoContent {
				t.Fatalf("%s reaction status = %d, want 204", method, resp.Status)
			}
		}
	}
	type delivery struct{ kind, id string }
	reactions := func(frames []frame) []delivery {
		var out []delivery
		for _, f := range frames {
			if f.T == "MESSAGE_REACTION_ADD" || f.T == "MESSAGE_REACTION_REMOVE" {
				var payload struct {
					MessageID string `json:"message_id"`
				}
				decode(t, f.D, &payload)
				out = append(out, delivery{f.T, payload.MessageID})
			}
		}
		return out
	}
	want := []delivery{
		{"MESSAGE_REACTION_ADD", privateMessage.ID}, {"MESSAGE_REACTION_REMOVE", privateMessage.ID},
		{"MESSAGE_REACTION_ADD", publicMessage.ID}, {"MESSAGE_REACTION_REMOVE", publicMessage.ID},
	}
	if got := reactions(e.untilSentinel(ocl, ou)); !slices.Equal(got, want) {
		t.Errorf("owner deliveries = %v, want %v", got, want)
	}
	if got := reactions(e.untilSentinel(mcl, mu)); !slices.Equal(got, want[2:]) {
		t.Errorf("member deliveries = %v, want %v", got, want[2:])
	}
}
