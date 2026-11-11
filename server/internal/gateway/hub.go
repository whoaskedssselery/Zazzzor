package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// dispatchLoop consumes the bus in order. An Overflow (the dispatcher fell
// behind and lost events) closes every connection with 4008 so the clients
// resync by reconnecting, then subscribes again; a closed subscription
// means the gateway or the bus is shutting down and closes them with 1012.
func (g *Gateway) dispatchLoop(ctx context.Context, ch <-chan events.Event) {
	defer close(g.dispatchDone)
	for {
		if !g.consume(ctx, ch) {
			g.mu.Lock()
			g.closed = true
			g.presence.stop()
			g.mu.Unlock()
			g.closeAll(CloseServiceRestart)
			return
		}
		g.log.Warn("gateway fell behind the event bus; closing every connection")
		// Subscribe again before closing, under the lock that registration
		// takes: a connection registered before the new subscription is
		// closed, one registered after it misses nothing.
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			return
		}
		var unsub func()
		ch, unsub = g.opts.Bus.Subscribe(g.opts.BusBuffer)
		g.unsub = unsub
		stale := make([]*conn, 0, len(g.conns))
		for c := range g.conns {
			stale = append(stale, c)
		}
		g.mu.Unlock()
		for _, c := range stale {
			c.closeWith(CloseOverloaded)
		}
	}
}

// consume dispatches events until Overflow (true) or the channel closes
// (false).
func (g *Gateway) consume(ctx context.Context, ch <-chan events.Event) bool {
	for e := range ch {
		if e.Type == events.Overflow {
			return true
		}
		g.dispatch(ctx, e)
	}
	return false
}

// dispatch delivers one event to the connections of its recipients,
// resolved now from the database. A failed resolution is logged (without
// detail to anyone) and drops the event; for a guild event the connections
// that hold the guild, or that needed its state, are closed with 4008 so
// their clients reconnect and resync instead of silently missing it.
func (g *Gateway) dispatch(ctx context.Context, e events.Event) {
	if ctx.Err() != nil {
		return // shutting down
	}
	if e.Type == events.SessionRevoked {
		g.revokeSessions(e)
		return
	}
	conns := g.connsByUser()
	if len(conns) == 0 {
		return // nobody to tell: no queries
	}
	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	recipients, err := g.recipients(ctx, e)
	if err != nil {
		if g.dropped(e, err) {
			g.resync(e.Audience.GuildID)
		}
		return
	}
	perRecipient := e.Type == events.GuildCreate || e.Type == events.GuildSync ||
		e.Audience.Kind == events.AudienceGuildSync
	var (
		shared  json.RawMessage
		guildID snowflake.ID
	)
	if perRecipient {
		if guildID = guildOf(e); guildID == 0 {
			g.dropped(e, errors.New("no guild id"))
			return
		}
	}
	seen := make(map[snowflake.ID]bool, len(recipients))
	for _, uid := range recipients {
		cs := conns[uid]
		if len(cs) == 0 || uid == e.ExcludeUserID || seen[uid] {
			continue
		}
		seen[uid] = true
		var d json.RawMessage
		if perRecipient {
			st, stErr := g.guildState(ctx, uid, guildID)
			if errors.Is(stErr, errNotInGuild) {
				continue // not a member (any more): no state to send
			}
			if stErr == nil {
				d, stErr = json.Marshal(st)
			}
			if stErr != nil {
				// This member needed the state: let it resync.
				if g.dropped(e, stErr) {
					for _, c := range cs {
						c.closeWith(CloseOverloaded)
					}
				}
				continue
			}
		} else {
			if shared == nil {
				if shared, err = json.Marshal(e.Payload); err != nil {
					g.dropped(e, err)
					return
				}
			}
			d = shared
		}
		for _, c := range cs {
			c.enqueue(outgoing{op: OpDispatch, t: e.Type, d: d})
			switch e.Type {
			case events.GuildCreate:
				c.hold(guildID)
			case events.GuildDelete:
				if p, ok := e.Payload.(guilds.GuildDeletePayload); ok {
					c.release(p.ID)
				}
			}
		}
	}
	switch e.Type {
	case events.GuildMemberAdd:
		g.joinPresence(e, recipients, conns)
	case events.GuildMemberRemove:
		g.leavePresence(ctx, e, recipients, conns)
	case events.GuildDelete:
		g.deletePresence(ctx, e, recipients, conns)
	}
}

// joinPresence follows GUILD_MEMBER_ADD: if the new member is online, the
// other members are told so. Those who already shared a server with them
// knew it; for them the update is a harmless repeat. The new member learns
// who is online from the presences of their GUILD_CREATE.
func (g *Gateway) joinPresence(e events.Event, members []snowflake.ID, conns map[snowflake.ID][]*conn) {
	p, ok := e.Payload.(guilds.MemberPayload)
	if !ok {
		return
	}
	joiner := p.Member.User.ID
	if len(g.OnlineAmong([]snowflake.ID{joiner})) == 0 {
		return
	}
	g.sendPresence(Presence{UserID: joiner, Status: StatusOnline}, members, joiner, conns)
}

// leavePresence follows GUILD_MEMBER_REMOVE (the removal has committed):
// the remaining members who share no server with the removed user any more
// see them offline if they were online, and the removed user sees those of
// them who are online go offline. Members still sharing a server hear
// nothing.
func (g *Gateway) leavePresence(ctx context.Context, e events.Event, members []snowflake.ID, conns map[snowflake.ID][]*conn) {
	p, ok := e.Payload.(guilds.MemberRemovePayload)
	if !ok {
		return
	}
	gone := p.User.ID
	removedOnline := len(g.OnlineAmong([]snowflake.ID{gone})) == 1
	if !removedOnline && len(conns[gone]) == 0 {
		return // nothing to say to anyone
	}
	peers, err := g.opts.Guilds.PeerIDs(ctx, gone)
	if err != nil {
		if g.dropped(e, err) {
			g.resync(e.Audience.GuildID)
		}
		return
	}
	var parted []snowflake.ID
	for _, id := range members {
		if id != gone && !slices.Contains(peers, id) {
			parted = append(parted, id)
		}
	}
	if removedOnline {
		g.sendPresence(Presence{UserID: gone, Status: StatusOffline}, parted, gone, conns)
	}
	for _, id := range g.OnlineAmong(parted) {
		g.sendPresence(Presence{UserID: id, Status: StatusOffline}, []snowflake.ID{gone}, id, conns)
	}
}

// deletePresence follows a GUILD_DELETE sent to the former members of a
// deleted guild (the deletion has committed): every pair of them that
// shares no guild any more sees each other go offline, as far as they are
// online and connected. A GUILD_DELETE to one user (who left or was
// removed) has no pair; GUILD_MEMBER_REMOVE covers it. One query for the
// whole guild.
func (g *Gateway) deletePresence(ctx context.Context, e events.Event, formers []snowflake.ID, conns map[snowflake.ID][]*conn) {
	if len(formers) < 2 {
		return
	}
	online := g.OnlineAmong(formers)
	var listening []snowflake.ID
	for _, id := range formers {
		if len(conns[id]) > 0 {
			listening = append(listening, id)
		}
	}
	if len(online) == 0 || len(listening) == 0 {
		return
	}
	shared, err := g.opts.Guilds.SharedPeers(ctx, listening, online)
	if err != nil {
		g.dropped(e, err) // the guild is gone: no connection holds it to resync
		return
	}
	for _, to := range listening {
		for _, id := range online {
			if id != to && !slices.Contains(shared[to], id) {
				g.sendPresence(Presence{UserID: id, Status: StatusOffline}, []snowflake.ID{to}, id, conns)
			}
		}
	}
}

// sendPresence queues PRESENCE_UPDATE p to the connections of to, except
// those of exclude.
func (g *Gateway) sendPresence(p Presence, to []snowflake.ID, exclude snowflake.ID, conns map[snowflake.ID][]*conn) {
	d, err := json.Marshal(p)
	if err != nil {
		return // two plain fields: cannot happen
	}
	for _, uid := range to {
		if uid == exclude {
			continue
		}
		for _, c := range conns[uid] {
			c.enqueue(outgoing{op: OpDispatch, t: events.PresenceUpdate, d: d})
		}
	}
}

// dropped counts and logs an event (or a per-recipient payload) that could
// not be delivered. While the gateway shuts down it does nothing and
// reports false: the query was cancelled by Close, which is not a failure.
func (g *Gateway) dropped(e events.Event, err error) bool {
	if g.ctx.Err() != nil {
		return false
	}
	g.m.dispatchErrors.Inc()
	g.log.Error("gateway dispatch", "type", e.Type, "err", err.Error())
	return true
}

// resync closes with 4008 every connection holding the state of guildID
// (none for 0): it missed an event of the guild, and reconnecting gives it
// a fresh READY.
func (g *Gateway) resync(guildID snowflake.ID) {
	if guildID == 0 {
		return
	}
	g.mu.Lock()
	var cs []*conn
	for c := range g.conns {
		if c.holds(guildID) {
			cs = append(cs, c)
		}
	}
	g.mu.Unlock()
	for _, c := range cs {
		c.closeWith(CloseOverloaded)
	}
}

// recipients resolves the audience of e to user IDs (ExcludeUserID is
// removed by the caller).
func (g *Gateway) recipients(ctx context.Context, e events.Event) ([]snowflake.ID, error) {
	a := e.Audience
	if e.Type == events.VoiceMove {
		if a.Kind != events.AudienceUsers || a.ChannelID <= 0 {
			return nil, errors.New("gateway: voice move without private destination audience")
		}
		viewers, err := g.opts.Channels.Viewers(ctx, a.ChannelID)
		if errors.Is(err, channels.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("gateway: read voice move destination viewers: %w", err)
		}
		return slices.DeleteFunc(slices.Clone(a.UserIDs), func(userID snowflake.ID) bool { return !slices.Contains(viewers, userID) }), nil
	}
	switch a.Kind {
	case events.AudienceGuild, events.AudienceGuildSync:
		return g.opts.Guilds.MemberIDs(ctx, a.GuildID)
	case events.AudienceChannel:
		ids, err := g.opts.Channels.Viewers(ctx, a.ChannelID)
		if errors.Is(err, channels.ErrNotFound) {
			return nil, nil // the channel is gone: nobody may see it
		}
		return ids, err
	case events.AudienceUsers:
		return a.UserIDs, nil
	case events.AudienceGuildPermission:
		if a.Permission == 0 {
			return nil, errors.New("audience without permission")
		}
		return g.opts.Guilds.MemberIDsWith(ctx, a.GuildID, a.Permission)
	case events.AudiencePeers:
		if len(a.UserIDs) == 0 {
			return nil, nil
		}
		ids, err := g.opts.Guilds.PeerIDs(ctx, a.UserIDs[0])
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ids, a.UserIDs[0]) {
			ids = append(ids, a.UserIDs[0])
		}
		return ids, nil
	default:
		return nil, errors.New("unknown audience")
	}
}

// guildOf is the guild a GUILD_CREATE or GUILD_SYNC is about.
func guildOf(e events.Event) snowflake.ID {
	switch p := e.Payload.(type) {
	case guilds.PublicSnapshot:
		return p.Guild.ID
	case guilds.GuildSyncPayload:
		return p.GuildID
	}
	return e.Audience.GuildID
}

// revokeSessions closes the connections of revoked sessions with 4004; the
// event itself is internal and never forwarded.
func (g *Gateway) revokeSessions(e events.Event) {
	p, ok := e.Payload.(events.SessionRevokedPayload)
	if !ok {
		return
	}
	g.mu.Lock()
	var cs []*conn
	for c := range g.conns {
		if slices.Contains(p.SessionIDs, c.sessionID) {
			cs = append(cs, c)
		}
	}
	g.mu.Unlock()
	for _, c := range cs {
		c.closeWith(CloseAuthFailed)
	}
}
