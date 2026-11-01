package gateway_test

import (
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestVoiceMoveFromHiddenChannelDoesNotExposeItsLeave(t *testing.T) {
	e, _ := voiceEnv(t)
	owner, _ := e.user("owner")
	moving, u := e.user("moving")
	viewer, observer := e.user("viewer")
	g, _ := newGuild(t, owner, "home")
	e.join(g, u)
	e.join(g, observer)
	hidden := voiceChannel(t, owner, g, "hidden")
	visible := voiceChannel(t, owner, g, "visible")
	overwrite(t, owner, hidden.ID, g.ID, 0, 0, permissions.ViewChannel)
	overwrite(t, owner, hidden.ID, u.ID, 1, permissions.ViewChannel, 0)
	client, _ := connect(t, viewer)
	e.connectVoice(moving, u, hidden.ID)
	e.connectVoice(moving, u, visible.ID)
	joined := false
	for _, event := range e.untilSentinel(client, observer) {
		if event.T != events.VoiceStateUpdate {
			continue
		}
		var payload struct {
			ChannelID string `json:"channel_id"`
			Left      bool   `json:"left"`
		}
		decode(t, event.D, &payload)
		if payload.ChannelID == hidden.ID || payload.Left {
			t.Fatalf("non-viewer received hidden move source: %+v", payload)
		}
		joined = joined || payload.ChannelID == visible.ID
	}
	if !joined {
		t.Fatal("non-viewer did not receive the public destination join")
	}
}

func TestGuildSyncDiscardsVoiceStatesOfNewlyHiddenChannels(t *testing.T) {
	e, _ := voiceEnv(t)
	owner, speaker := e.user("owner")
	viewer, observer := e.user("viewer")
	g, _ := newGuild(t, owner, "home")
	e.join(g, observer)
	channel := voiceChannel(t, owner, g, "voice")
	client, _ := connect(t, viewer)
	e.connectVoice(owner, speaker, channel.ID)
	client.waitFor(events.VoiceStateUpdate)
	overwrite(t, owner, channel.ID, observer.ID, 1, 0, permissions.ViewChannel)
	var snapshot struct {
		Channels    []channelBody `json:"channels"`
		VoiceStates []struct {
			ChannelID string `json:"channel_id"`
		} `json:"voice_states"`
	}
	decode(t, client.waitFor(events.GuildSync).D, &snapshot)
	if len(snapshot.VoiceStates) != 0 || slices.ContainsFunc(snapshot.Channels, func(c channelBody) bool { return c.ID == channel.ID }) {
		t.Fatalf("GUILD_SYNC exposed newly hidden voice channel: %+v", snapshot)
	}
	if !e.inVoice(g, speaker) {
		t.Fatal("test removed the speaker instead of filtering the receiver")
	}
}
