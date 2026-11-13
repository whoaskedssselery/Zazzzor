package app

import (
	"context"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships"
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

// accountDisabled takes a disabled account out of voice at once: its
// token may still be valid for a few minutes, and admission refuses it
// from now on.
func (p *voiceCleanup) accountDisabled(ctx context.Context, userID snowflake.ID) {
	if p.voice != nil {
		p.voice.RemoveUser(ctx, userID)
	}
}

// Publish implements events.Publisher. GUILD_DELETE goes to the users who
// are no longer in the guild: every member when it was deleted, only the
// removed one when a member left or was kicked or banned. So exactly those
// users leave its voice channels.
func (p *voiceCleanup) Publish(ctx context.Context, e events.Event) {
	if p.voice != nil {
		if e.Type == events.GuildSync && e.Audience.Kind == events.AudienceGuildSync {
			p.voice.RefreshGuild(e.Audience.GuildID)
		}
		switch pl := e.Payload.(type) {
		case guilds.GuildDeletePayload:
			if e.Type == events.GuildDelete {
				p.voice.RemoveMembers(ctx, pl.ID, e.Audience.UserIDs)
			}
		case channels.PublicChannel:
			if e.Type == events.ChannelDelete {
				p.voice.RemoveChannel(ctx, pl.ID)
			}
		case relationships.Relationship:
			// A block ends a 1:1 call between the two, as it refuses a new
			// token. Off this goroutine: the check reads the database.
			if e.Type == events.RelationshipAdd && pl.Type == relationships.Blocked && len(e.Audience.UserIDs) == 1 {
				go p.voice.RecheckCalls(context.WithoutCancel(ctx), []snowflake.ID{e.Audience.UserIDs[0], pl.ID})
			}
		case channels.DMChannelDelete:
			// Closed, left or removed: only the last two end the recipient's
			// part in the call, which a lookup tells apart. Not on this goroutine:
			// Publish must not wait on the database.
			if e.Type == events.DMChannelDelete && len(e.Audience.UserIDs) == 1 {
				userID := e.Audience.UserIDs[0]
				go p.voice.RecheckPrivate(context.WithoutCancel(ctx), pl.ID, userID)
			}
		}
	}
	if p.next != nil {
		p.next.Publish(ctx, e)
	}
}
