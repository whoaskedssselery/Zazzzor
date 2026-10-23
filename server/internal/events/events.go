// Package events is the in-process event bus that connects the domain modules
// (which publish after a transaction commits) to the gateway (which fans the
// events out to connected clients).
package events

import (
	"context"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Event types. SESSION_REVOKED and OVERFLOW are internal and never sent to
// clients as-is.
const (
	GuildCreate       = "GUILD_CREATE"
	GuildUpdate       = "GUILD_UPDATE"
	GuildDelete       = "GUILD_DELETE"
	GuildSync         = "GUILD_SYNC"
	GuildMemberAdd    = "GUILD_MEMBER_ADD"
	GuildMemberUpdate = "GUILD_MEMBER_UPDATE"
	GuildMemberRemove = "GUILD_MEMBER_REMOVE"
	GuildBanAdd       = "GUILD_BAN_ADD"
	GuildBanRemove    = "GUILD_BAN_REMOVE"
	GuildRoleCreate   = "GUILD_ROLE_CREATE"
	GuildRoleUpdate   = "GUILD_ROLE_UPDATE"
	GuildRoleDelete   = "GUILD_ROLE_DELETE"
	ChannelCreate     = "CHANNEL_CREATE"
	ChannelUpdate     = "CHANNEL_UPDATE"
	ChannelDelete     = "CHANNEL_DELETE"
	MessageCreate     = "MESSAGE_CREATE"
	MessageUpdate     = "MESSAGE_UPDATE"
	MessageDelete     = "MESSAGE_DELETE"
	TypingStart       = "TYPING_START"
	PresenceUpdate    = "PRESENCE_UPDATE"
	UserUpdate        = "USER_UPDATE"
	SessionRevoked    = "SESSION_REVOKED"

	// Overflow is the last event a subscriber receives when it fell behind
	// and was dropped by the bus.
	Overflow = "OVERFLOW"
)

// AudienceKind says who should receive an event.
type AudienceKind int

const (
	// AudienceGuild is every member of GuildID.
	AudienceGuild AudienceKind = iota + 1
	// AudienceChannel is the members of GuildID who have VIEW_CHANNEL in ChannelID.
	AudienceChannel
	// AudienceUsers is exactly UserIDs.
	AudienceUsers
	// AudienceGuildSync is every member of GuildID, each getting their own
	// snapshot of the guild.
	AudienceGuildSync
	// AudiencePeers is UserIDs[0] and everyone sharing a guild with them; the
	// gateway resolves it through guilds.Directory.
	AudiencePeers
	// AudienceGuildPermission is the members of GuildID whose guild-level
	// permissions hold Permission when the event is sent (e.g. the
	// moderators for bans); the gateway resolves it through
	// guilds.Directory.
	AudienceGuildPermission
)

// Audience selects the recipients of an event.
type Audience struct {
	Kind      AudienceKind
	GuildID   snowflake.ID
	ChannelID snowflake.ID
	UserIDs   []snowflake.ID
	// Permission is what AudienceGuildPermission requires.
	Permission permissions.Permission
}

// Event is one thing that happened.
type Event struct {
	Type     string
	Audience Audience
	Payload  any
	// ExcludeUserID, when non-zero, is not sent the event.
	ExcludeUserID snowflake.ID
}

// Publisher is what modules depend on to announce events. Implementations
// must not block.
type Publisher interface {
	Publish(ctx context.Context, e Event)
}

// SessionRevokedPayload is the payload of a SessionRevoked event: the gateway
// closes the connections that authenticated with these sessions.
type SessionRevokedPayload struct {
	UserID     snowflake.ID
	SessionIDs []snowflake.ID
}
