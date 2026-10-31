package app

import (
	"context"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// voiceCleanup is the publisher of the guilds and channels modules: it
// passes every event on, and first takes people out of voice when an event
// says they lost a guild or a channel is gone. The events are published
// after the change committed, so nothing is removed for a change that
// rolled back, and the modules know nothing about voice.
type voiceCleanup struct {
	next events.Publisher // nil: events are dropped
	// voice is set once the voice service exists, before anything
	// publishes.
	voice *voice.Service
}

// Publish implements events.Publisher. GUILD_DELETE goes to the users who
// are no longer in the guild: every member when it was deleted, only the
// removed one when a member left or was kicked or banned. So exactly those
// users leave its voice channels.
func (p *voiceCleanup) Publish(ctx context.Context, e events.Event) {
	if p.voice != nil {
		switch pl := e.Payload.(type) {
		case guilds.GuildDeletePayload:
			if e.Type == events.GuildDelete {
				p.voice.RemoveMembers(ctx, pl.ID, e.Audience.UserIDs)
			}
		case channels.PublicChannel:
			if e.Type == events.ChannelDelete {
				p.voice.RemoveChannel(ctx, pl.ID)
			}
		}
	}
	if p.next != nil {
		p.next.Publish(ctx, e)
	}
}
