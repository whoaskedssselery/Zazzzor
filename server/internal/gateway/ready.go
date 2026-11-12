package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/notifysettings"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// errNotInGuild: the guild is gone or the user is no longer a member of it.
var errNotInGuild = errors.New("gateway: not in guild")

// guildState builds the GuildState of guildID as userID sees it; READY,
// GUILD_CREATE and GUILD_SYNC all use it. A guild the user is not in (any
// more) is errNotInGuild.
func (g *Gateway) guildState(ctx context.Context, userID, guildID snowflake.ID) (GuildState, error) {
	snap, err := g.opts.Guilds.Snapshot(ctx, guildID)
	if errors.Is(err, guilds.ErrNotFound) {
		return GuildState{}, errNotInGuild
	}
	if err != nil {
		return GuildState{}, err
	}
	i := slices.IndexFunc(snap.Members, func(m guilds.Member) bool { return m.User.ID == userID })
	if i < 0 {
		return GuildState{}, errNotInGuild
	}
	visible, err := g.opts.Channels.VisibleChannels(ctx, userID, guildID)
	if errors.Is(err, guilds.ErrNotMember) || errors.Is(err, guilds.ErrNotFound) {
		return GuildState{}, errNotInGuild
	}
	if err != nil {
		return GuildState{}, err
	}

	pub := snap.Public()
	st := GuildState{Guild: pub.Guild, Roles: pub.Roles, MyMember: pub.Members[i],
		Members:  pub.Members[:min(len(pub.Members), MaxMembers)],
		Channels: make([]channels.PublicChannel, len(visible)), Emojis: snap.Emojis}
	if st.Emojis == nil {
		st.Emojis = []guilds.Emoji{}
	}
	for j, c := range visible {
		st.Channels[j] = c.Public()
	}
	st.ReadStates = []messages.ReadState{}
	if g.opts.ReadStates != nil && len(visible) > 0 {
		channelIDs := make([]snowflake.ID, len(visible))
		for j, c := range visible {
			channelIDs[j] = c.ID
		}
		states, err := g.opts.ReadStates.ReadStates(ctx, userID, channelIDs)
		if err != nil {
			return GuildState{}, fmt.Errorf("gateway: read states: %w", err)
		}
		if states != nil {
			st.ReadStates = states
		}
	}
	memberIDs := make([]snowflake.ID, len(snap.Members))
	for j, m := range snap.Members {
		memberIDs[j] = m.User.ID
	}
	online := g.OnlineAmong(memberIDs)
	st.Presences = make([]Presence, len(online))
	for j, id := range online {
		st.Presences[j] = Presence{UserID: id, Status: StatusOnline}
	}
	st.VoiceStates = g.voiceStates(guildID, visible)
	return st, nil
}

// voiceStates returns the voice states of guildID in the channels of
// visible, which the receiver may view; the others stay hidden.
func (g *Gateway) voiceStates(guildID snowflake.ID, visible []channels.Channel) []voice.PublicState {
	out := []voice.PublicState{}
	if g.opts.Voice == nil {
		return out
	}
	for _, vs := range g.opts.Voice.GuildStates(guildID) {
		if slices.ContainsFunc(visible, func(c channels.Channel) bool { return c.ID == vs.ChannelID }) {
			out = append(out, vs.Public())
		}
	}
	return out
}

// ready builds READY for user: every guild they are in, as they see it,
// and the presence of everyone online who shares a guild with them
// (themselves included).
func (g *Gateway) ready(ctx context.Context, user auth.User) (Ready, error) {
	ids, err := g.opts.Guilds.GuildIDsOf(ctx, user.ID)
	if err != nil {
		return Ready{}, err
	}
	r := Ready{User: user.Public(), Guilds: make([]GuildState, 0, len(ids)), Presences: []Presence{},
		ReadStates: []messages.ReadState{}, Relationships: []relationships.Relationship{},
		UserGuildSettings: []notifysettings.Settings{}, PrivateVoiceStates: []voice.PublicState{}, Calls: []voice.Call{}}
	if g.opts.NotifySettings != nil {
		list, listErr := g.opts.NotifySettings.List(ctx, user.ID)
		if listErr != nil {
			return Ready{}, fmt.Errorf("gateway: read notification settings: %w", listErr)
		}
		if list != nil {
			r.UserGuildSettings = list
		}
	}
	if g.opts.Relationships != nil {
		list, listErr := g.opts.Relationships.List(ctx, user.ID)
		if listErr != nil {
			return Ready{}, fmt.Errorf("gateway: read relationships: %w", listErr)
		}
		if list != nil {
			r.Relationships = list
		}
	}
	for _, id := range ids {
		st, stErr := g.guildState(ctx, user.ID, id)
		if errors.Is(stErr, errNotInGuild) {
			continue // left meanwhile
		}
		if stErr != nil {
			return Ready{}, stErr
		}
		r.Guilds = append(r.Guilds, st)
		r.ReadStates = append(r.ReadStates, st.ReadStates...)
	}
	if privateErr := g.readyPrivate(ctx, user.ID, &r); privateErr != nil {
		return Ready{}, privateErr
	}
	peers, err := g.opts.Guilds.PeerIDs(ctx, user.ID)
	if err != nil {
		return Ready{}, err
	}
	if !slices.Contains(peers, user.ID) {
		peers = append(peers, user.ID)
		slices.Sort(peers)
	}
	for _, id := range g.OnlineAmong(peers) {
		r.Presences = append(r.Presences, Presence{UserID: id, Status: StatusOnline})
	}
	return r, nil
}

// readyPrivate adds open private channels and only their stored read markers.
func (g *Gateway) readyPrivate(ctx context.Context, user snowflake.ID, r *Ready) error {
	r.PrivateChannels = []channels.DMChannel{}
	if g.opts.PrivateChannels == nil {
		return nil
	}
	list, err := g.opts.PrivateChannels.PrivateChannels(ctx, user)
	if err != nil {
		return fmt.Errorf("gateway: list private channels: %w", err)
	}
	if len(list) == 0 {
		return nil
	}
	r.PrivateChannels = list
	if g.opts.Voice != nil {
		ids := make([]snowflake.ID, len(list))
		for i, c := range list {
			ids[i] = c.ID
		}
		calls, states := g.opts.Voice.PrivateCalls(ids)
		r.Calls = calls
		for _, st := range states {
			r.PrivateVoiceStates = append(r.PrivateVoiceStates, st.Public())
		}
	}
	if g.opts.ReadStates == nil {
		return nil
	}
	ids := make([]snowflake.ID, len(list))
	for i, channel := range list {
		ids[i] = channel.ID
	}
	states, err := g.opts.ReadStates.ReadStates(ctx, user, ids)
	if err != nil {
		return fmt.Errorf("gateway: read private markers: %w", err)
	}
	r.ReadStates = append(r.ReadStates, states...)
	return nil
}
