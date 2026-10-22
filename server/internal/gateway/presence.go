package gateway

import (
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// presence counts the connections of each user. A user goes online with
// their first connection and offline PresenceGrace after their last one
// closed, unless they reconnect within it. Every field is guarded by
// Gateway.mu; the changes are published on the bus, so they reach peers
// through the dispatcher in order with every other event.
type presence struct {
	conns   map[snowflake.ID]int
	online  map[snowflake.ID]bool
	pending map[snowflake.ID]pendingOffline
	gen     uint64
	stopped bool
}

type pendingOffline struct {
	timer *time.Timer
	gen   uint64
}

func newPresence() presence {
	return presence{conns: make(map[snowflake.ID]int), online: make(map[snowflake.ID]bool),
		pending: make(map[snowflake.ID]pendingOffline)}
}

// stop cancels the pending offline timers; nothing is announced any more.
func (p *presence) stop() {
	p.stopped = true
	for id, po := range p.pending {
		po.timer.Stop()
		delete(p.pending, id)
	}
}

// presenceConnected runs under g.mu when a connection of userID registers.
func (g *Gateway) presenceConnected(userID snowflake.ID) {
	p := &g.presence
	p.conns[userID]++
	if p.conns[userID] > 1 {
		return
	}
	if po, ok := p.pending[userID]; ok {
		// Back within the grace period: still online, nothing to say.
		po.timer.Stop()
		delete(p.pending, userID)
		return
	}
	if !p.online[userID] {
		p.online[userID] = true
		g.publishPresence(userID, StatusOnline)
	}
}

// presenceDisconnected runs under g.mu when a connection of userID ends.
func (g *Gateway) presenceDisconnected(userID snowflake.ID) {
	p := &g.presence
	p.conns[userID]--
	if p.conns[userID] > 0 {
		return
	}
	delete(p.conns, userID)
	if p.stopped {
		return
	}
	p.gen++
	gen := p.gen
	p.pending[userID] = pendingOffline{gen: gen, timer: time.AfterFunc(g.opts.PresenceGrace, func() {
		g.presenceExpired(userID, gen)
	})}
}

// presenceExpired announces offline unless the timer was cancelled or
// replaced meanwhile.
func (g *Gateway) presenceExpired(userID snowflake.ID, gen uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := &g.presence
	if po, ok := p.pending[userID]; !ok || po.gen != gen || p.stopped {
		return
	}
	delete(p.pending, userID)
	delete(p.online, userID)
	g.publishPresence(userID, StatusOffline)
}

// publishPresence announces a status to the user's peers (not to the user).
// Publish never blocks, so it is safe under g.mu.
func (g *Gateway) publishPresence(userID snowflake.ID, status string) {
	g.opts.Bus.Publish(g.ctx, events.Event{Type: events.PresenceUpdate,
		Audience:      events.Audience{Kind: events.AudiencePeers, UserIDs: []snowflake.ID{userID}},
		ExcludeUserID: userID,
		Payload:       Presence{UserID: userID, Status: status}})
}

// onlineAmong returns the online users among ids, in their order.
func (g *Gateway) onlineAmong(ids []snowflake.ID) []snowflake.ID {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]snowflake.ID, 0, len(ids))
	for _, id := range ids {
		if g.presence.online[id] {
			out = append(out, id)
		}
	}
	return out
}
