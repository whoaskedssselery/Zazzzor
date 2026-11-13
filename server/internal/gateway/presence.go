package gateway

import (
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// presence counts the connections of each user. A user goes online with
// their first connection and offline PresenceGrace after their last one
// closed, unless they reconnect within it. Peers see the status the user
// chose (online, idle or dnd) and their status line; an invisible user
// looks offline to them. Every field is guarded by
// Gateway.mu; the changes are published on the bus, so they reach peers
// through the dispatcher in order with every other event.
type presence struct {
	conns   map[snowflake.ID]int
	online  map[snowflake.ID]bool
	pending map[snowflake.ID]pendingOffline
	// prefs is the chosen status of each user with a connection or in
	// the grace period.
	prefs   map[snowflake.ID]presencePref
	gen     uint64
	stopped bool
}

type presencePref struct {
	status string // online, idle, dnd or invisible
	custom *string
}

type pendingOffline struct {
	timer *time.Timer
	gen   uint64
}

func newPresence() presence {
	return presence{conns: make(map[snowflake.ID]int), online: make(map[snowflake.ID]bool),
		pending: make(map[snowflake.ID]pendingOffline), prefs: make(map[snowflake.ID]presencePref)}
}

// stop cancels the pending offline timers; nothing is announced any more.
func (p *presence) stop() {
	p.stopped = true
	for id, po := range p.pending {
		po.timer.Stop()
		delete(p.pending, id)
	}
}

// presenceConnected runs under g.mu when a connection of user registers.
// The status the user chose is taken from the first connection; later
// changes arrive as PresenceSet.
func (g *Gateway) presenceConnected(user auth.User) {
	p := &g.presence
	userID := user.ID
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
		p.prefs[userID] = presencePref{status: user.Status, custom: user.CustomStatus}
		if shown := g.shownLocked(userID); shown.Status != StatusOffline {
			g.publishPresence(shown)
		}
	}
}

// setPresence takes a new chosen status of a user and announces what their
// peers now see, if that changed. A user with no connection is skipped:
// their next connection reads the status from their account.
func (g *Gateway) setPresence(ps events.PresenceSetPayload) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := &g.presence
	if !p.online[ps.UserID] || p.stopped {
		return
	}
	before := g.shownLocked(ps.UserID)
	p.prefs[ps.UserID] = presencePref{status: ps.Status, custom: ps.CustomStatus}
	if after := g.shownLocked(ps.UserID); !samePresence(before, after) {
		g.publishPresence(after)
	}
}

// shownLocked is the presence peers see for userID; g.mu is held.
func (g *Gateway) shownLocked(userID snowflake.ID) Presence {
	pref := g.presence.prefs[userID]
	if !g.presence.online[userID] || pref.status == auth.StatusInvisible {
		return Presence{UserID: userID, Status: StatusOffline}
	}
	status := pref.status
	if status != StatusIdle && status != StatusDND {
		status = StatusOnline
	}
	return Presence{UserID: userID, Status: status, CustomStatus: pref.custom}
}

func samePresence(a, b Presence) bool {
	if a.Status != b.Status || (a.CustomStatus == nil) != (b.CustomStatus == nil) {
		return false
	}
	return a.CustomStatus == nil || *a.CustomStatus == *b.CustomStatus
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
	wasShown := g.shownLocked(userID).Status != StatusOffline
	delete(p.online, userID)
	delete(p.prefs, userID)
	if wasShown {
		g.publishPresence(Presence{UserID: userID, Status: StatusOffline})
	}
}

// publishPresence announces a presence to the user's peers (not to the
// user). Publish never blocks, so it is safe under g.mu.
func (g *Gateway) publishPresence(p Presence) {
	g.opts.Bus.Publish(g.ctx, events.Event{Type: events.PresenceUpdate,
		Audience:      events.Audience{Kind: events.AudiencePeers, UserIDs: []snowflake.ID{p.UserID}},
		ExcludeUserID: p.UserID,
		Payload:       p})
}

// PresencesAmong returns what peers see of those of ids who show as online
// (invisible users do not), in their order. Users in their presence grace
// period are still online.
func (g *Gateway) PresencesAmong(ids []snowflake.ID) []Presence {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Presence, 0, len(ids))
	for _, id := range ids {
		if p := g.shownLocked(id); p.Status != StatusOffline {
			out = append(out, p)
		}
	}
	return out
}

// OnlineAmong returns those of ids who show as online, in their order.
func (g *Gateway) OnlineAmong(ids []snowflake.ID) []snowflake.ID {
	shown := g.PresencesAmong(ids)
	out := make([]snowflake.ID, len(shown))
	for i, p := range shown {
		out[i] = p.UserID
	}
	return out
}
