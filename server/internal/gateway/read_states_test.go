package gateway_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
)

func TestGatewayReadStatesExcludeHiddenChannels(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	owner, _ := e.user("owner")
	reader, member := e.user("reader")
	g, public := newGuild(t, owner, "home")
	e.join(g, member)
	private := hiddenChannel(t, owner, g, "private")
	publicMessage := post(t, reader, public.ID, "public")
	// Retain a hidden row to exercise the gateway filter independently of permission cleanup.
	guildID := parseID(t, g.ID)
	if _, err := e.pool.Exec(t.Context(), `
		INSERT INTO read_states (user_id, channel_id, guild_id, last_read_id, mention_count)
		VALUES ($1, $2, $3, 0, 1)`, member.id(t), parseID(t, private.ID), guildID); err != nil {
		t.Fatalf("seed hidden read state: %v", err)
	}

	cl, ready := connect(t, reader)
	e.untilSentinel(cl, member)

	check := func(t *testing.T, states []readStateBody) {
		t.Helper()
		if !slices.Contains(states, readStateBody{ChannelID: public.ID, LastReadID: publicMessage.ID}) {
			t.Errorf("read_states = %+v, want the public channel's read position at %s", states, publicMessage.ID)
		}
		if slices.ContainsFunc(states, func(state readStateBody) bool { return state.ChannelID == private.ID }) {
			t.Errorf("read_states = %+v, want no state for hidden channel %s", states, private.ID)
		}
	}

	t.Run("GUILD_SYNC", func(t *testing.T) {
		e.bus.Publish(t.Context(), events.Event{Type: events.GuildSync,
			Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID}})
		var state guildStateBody
		decode(t, cl.waitFor(events.GuildSync).D, &state)
		check(t, state.ReadStates)
	})
	t.Run("READY", func(t *testing.T) {
		check(t, ready.ReadStates)
		state, ok := ready.guild(g.ID)
		if !ok {
			t.Fatalf("READY guilds = %+v, want guild %s", ready.Guilds, g.ID)
		}
		check(t, state.ReadStates)
	})
}

func TestMessageAckReachesOnlyReadersConnections(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	owner, peer := e.user("owner")
	reader, member := e.user("reader")
	otherTab := e.login("reader")
	g, ch := newGuild(t, owner, "home")
	e.join(g, member)
	message := post(t, owner, ch.ID, "hello")

	first, _ := connect(t, reader)
	second, _ := connect(t, otherTab)
	observer, _ := connect(t, owner)
	path := "/api/v1/channels/" + ch.ID + "/messages/" + message.ID + "/ack"
	expect(t, reader.Do(http.MethodPost, path, nil), http.StatusNoContent)

	want := []readStateBody{{ChannelID: ch.ID, LastReadID: message.ID}}
	for _, tab := range []*wsClient{first, second} {
		if got := messageAcks(t, e.untilSentinel(tab, member)); !slices.Equal(got, want) {
			t.Errorf("reader's MESSAGE_ACK payloads = %+v, want %+v", got, want)
		}
	}
	if got := messageAcks(t, e.untilSentinel(observer, peer)); len(got) != 0 {
		t.Errorf("peer's MESSAGE_ACK payloads = %+v, want none", got)
	}
}

// messageAcks extracts the read positions the gateway sent before the sentinel.
func messageAcks(t *testing.T, frames []frame) []readStateBody {
	t.Helper()
	var acks []readStateBody
	for _, f := range frames {
		if f.T == events.MessageAck {
			var state readStateBody
			decode(t, f.D, &state)
			acks = append(acks, state)
		}
	}
	return acks
}
