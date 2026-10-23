package gateway

import (
	"context"
	"errors"
	"slices"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
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
		Channels: make([]channels.PublicChannel, len(visible))}
	for j, c := range visible {
		st.Channels[j] = c.Public()
	}
	memberIDs := make([]snowflake.ID, len(snap.Members))
	for j, m := range snap.Members {
		memberIDs[j] = m.User.ID
	}
	online := g.onlineAmong(memberIDs)
	st.Presences = make([]Presence, len(online))
	for j, id := range online {
		st.Presences[j] = Presence{UserID: id, Status: StatusOnline}
	}
	return st, nil
}

// ready builds READY for user: every guild they are in, as they see it,
// and the presence of everyone online who shares a guild with them
// (themselves included).
func (g *Gateway) ready(ctx context.Context, user auth.User) (Ready, error) {
	ids, err := g.opts.Guilds.GuildIDsOf(ctx, user.ID)
	if err != nil {
		return Ready{}, err
	}
	r := Ready{User: user.Public(), Guilds: make([]GuildState, 0, len(ids)), Presences: []Presence{}}
	for _, id := range ids {
		st, stErr := g.guildState(ctx, user.ID, id)
		if errors.Is(stErr, errNotInGuild) {
			continue // left meanwhile
		}
		if stErr != nil {
			return Ready{}, stErr
		}
		r.Guilds = append(r.Guilds, st)
	}
	peers, err := g.opts.Guilds.PeerIDs(ctx, user.ID)
	if err != nil {
		return Ready{}, err
	}
	if !slices.Contains(peers, user.ID) {
		peers = append(peers, user.ID)
		slices.Sort(peers)
	}
	for _, id := range g.onlineAmong(peers) {
		r.Presences = append(r.Presences, Presence{UserID: id, Status: StatusOnline})
	}
	return r, nil
}
