package gateway

import (
	"context"
	"encoding/json"
	"errors"
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
func (g *Gateway) dispatchLoop(ch <-chan events.Event) {
	defer close(g.dispatchDone)
	for {
		if !g.consume(ch) {
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
func (g *Gateway) consume(ch <-chan events.Event) bool {
	for e := range ch {
		if e.Type == events.Overflow {
			return true
		}
		g.dispatch(e)
	}
	return false
}

// dispatch delivers one event to the connections of its recipients,
// resolved now from the database. A failed resolution is logged (without
// detail to anyone) and drops the event.
func (g *Gateway) dispatch(e events.Event) {
	if g.ctx.Err() != nil {
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
	ctx, cancel := context.WithTimeout(g.ctx, dispatchTimeout)
	defer cancel()

	recipients, err := g.recipients(ctx, e)
	if err != nil {
		g.dropped(e, err)
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
			if stErr != nil {
				g.dropped(e, stErr)
				continue
			}
			if d, stErr = json.Marshal(st); stErr != nil {
				g.dropped(e, stErr)
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
		}
	}
}

func (g *Gateway) dropped(e events.Event, err error) {
	if g.ctx.Err() != nil {
		return // the query was cancelled by Close: not a failure
	}
	g.m.dispatchErrors.Inc()
	g.log.Error("gateway dispatch", "type", e.Type, "err", err.Error())
}

// recipients resolves the audience of e to user IDs (ExcludeUserID is
// removed by the caller).
func (g *Gateway) recipients(ctx context.Context, e events.Event) ([]snowflake.ID, error) {
	a := e.Audience
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
