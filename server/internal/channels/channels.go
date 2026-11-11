// Package channels owns the channels and categories of the guilds and their
// permission overwrites, and resolves what a member may do in a channel.
package channels

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// Limits.
const (
	// MaxChannels is how many channels (categories included) a guild may have.
	MaxChannels = 500
	// MaxTopic is the longest topic, in characters (runes).
	MaxTopic = 1024
)

// Channel writes per user, shared: 30 per minute.
var (
	writeRate  = rate.Every(time.Minute / 30)
	writeBurst = 30
	writeTTL   = 2 * time.Minute
)

// Type is the kind of a channel.
type Type int16

// Channel types, numbered as in Discord.
const (
	TypeText     Type = 0
	TypeVoice    Type = 2
	TypeCategory Type = 4
)

func (t Type) valid() bool { return t == TypeText || t == TypeVoice || t == TypeCategory }

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	// ErrNotFound: the channel does not exist, or the user may not see it;
	// both look the same to clients (404).
	ErrNotFound        = errors.New("channels: not found")
	ErrMissingPerm     = errors.New("channels: missing permission")
	ErrTooManyChannels = errors.New("channels: too many channels")
)

// ValidationError lists the request fields that failed validation.
type ValidationError struct {
	Fields []httpx.FieldError
}

func (e *ValidationError) Error() string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Field
	}
	return "channels: invalid " + strings.Join(names, ", ")
}

func invalid(field, msg string) error {
	return &ValidationError{Fields: []httpx.FieldError{{Field: field, Message: msg}}}
}

// ToProblem maps the errors of this package (and of guilds) to their HTTP
// problem; any other error is returned unchanged.
func ToProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		return httpx.BadRequest("validation_failed", "some fields are invalid", verr.Fields...)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound()
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	case errors.Is(err, ErrTooManyChannels):
		return httpx.BadRequest("too_many_channels", fmt.Sprintf("a server can have at most %d channels", MaxChannels))
	}
	return guilds.ToProblem(err)
}

// Channel is a channel or category. Overwrites are the effective ones: the
// category's when PermissionSynced, the channel's own otherwise.
type Channel struct {
	ID               snowflake.ID
	GuildID          snowflake.ID
	Type             Type
	Name             string
	Topic            *string
	LastMessageID    *snowflake.ID
	ParentID         *snowflake.ID
	Position         int
	PermissionSynced bool
	Overwrites       []permissions.Overwrite
}

// PublicOverwrite is the JSON form of an overwrite.
type PublicOverwrite struct {
	ID    snowflake.ID           `json:"id"`
	Type  int                    `json:"type"`
	Allow permissions.Permission `json:"allow"`
	Deny  permissions.Permission `json:"deny"`
}

// PublicChannel is the JSON form of a Channel (schema Channel).
type PublicChannel struct {
	ID                   snowflake.ID      `json:"id"`
	GuildID              snowflake.ID      `json:"guild_id"`
	Type                 int               `json:"type"`
	Name                 string            `json:"name"`
	Topic                *string           `json:"topic"`
	LastMessageID        *snowflake.ID     `json:"last_message_id"`
	ParentID             *snowflake.ID     `json:"parent_id"`
	Position             int               `json:"position"`
	PermissionSynced     bool              `json:"permission_synced"`
	PermissionOverwrites []PublicOverwrite `json:"permission_overwrites"`
}

// Public returns the JSON form of c.
func (c Channel) Public() PublicChannel {
	ows := make([]PublicOverwrite, len(c.Overwrites))
	for i, o := range c.Overwrites {
		ows[i] = PublicOverwrite{ID: o.ID, Type: int(o.Type), Allow: o.Allow, Deny: o.Deny}
	}
	return PublicChannel{ID: c.ID, GuildID: c.GuildID, Type: int(c.Type), Name: c.Name, Topic: c.Topic,
		LastMessageID: c.LastMessageID, ParentID: c.ParentID, Position: c.Position, PermissionSynced: c.PermissionSynced,
		PermissionOverwrites: ows}
}

// Resolver answers what members may do in channels; the messages module and
// the gateway use it. Every call reads the database (no cache: the scale is
// a few dozen members).
type Resolver interface {
	// Permissions returns userID's permissions in channelID and the guild of
	// the channel. A missing channel, a non-member and a channel the user
	// may not view are all ErrNotFound.
	Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error)
	// VisibleChannels returns the channels of guildID that userID may view,
	// in display order; a non-member is guilds.ErrNotMember.
	VisibleChannels(ctx context.Context, userID, guildID snowflake.ID) ([]Channel, error)
	// Viewers returns the members who may view channelID, ascending.
	Viewers(ctx context.Context, channelID snowflake.ID) ([]snowflake.ID, error)
}

var _ Resolver = (*Service)(nil)

// Guilds is what channels needs from the guilds module.
type Guilds interface {
	LockGuild(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error
	SubjectIn(ctx context.Context, db guilds.DB, guildID, userID snowflake.ID) (permissions.Subject, error)
	AccessIn(ctx context.Context, db guilds.DB, guildID snowflake.ID) (guilds.Access, error)
}

var _ Guilds = (*guilds.Service)(nil)

// Options configures a Service. Pool, IDs and Guilds are required.
type Options struct {
	Pool      *pgxpool.Pool
	IDs       *snowflake.Generator
	Guilds    Guilds
	Publisher events.Publisher // nil: events are dropped
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// WriteLimiter limits channel and overwrite writes per user; nil takes
	// the default (30 per minute).
	WriteLimiter *ratelimit.Limiter
}

// Service implements channels, categories and overwrites.
type Service struct {
	pool         *pgxpool.Pool
	q            *store.Queries
	ids          *snowflake.Generator
	guilds       Guilds
	pub          events.Publisher
	now          func() time.Time
	writeLimiter *ratelimit.Limiter
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: store.New(), ids: o.IDs, guilds: o.Guilds, pub: o.Publisher,
		now: o.Now, writeLimiter: o.WriteLimiter}
	if s.now == nil {
		s.now = time.Now
	}
	if s.writeLimiter == nil {
		s.writeLimiter = ratelimit.New(writeRate, writeBurst, writeTTL)
	}
	return s
}

// Bootstrapper creates the default channels of a new guild; it implements
// guilds.ChannelsBootstrapper. It is separate from Service because the
// guilds service it plugs into is built first.
type Bootstrapper struct {
	ids *snowflake.Generator
	now func() time.Time
	q   *store.Queries
}

var _ guilds.ChannelsBootstrapper = (*Bootstrapper)(nil)

// NewBootstrapper returns a Bootstrapper taking IDs from ids; nil now means
// time.Now.
func NewBootstrapper(ids *snowflake.Generator, now func() time.Time) *Bootstrapper {
	if now == nil {
		now = time.Now
	}
	return &Bootstrapper{ids: ids, now: now, q: store.New()}
}

// Names of the default channels.
const (
	DefaultTextCategory  = "Текстовые каналы"
	DefaultTextChannel   = "общий"
	DefaultVoiceCategory = "Голосовые каналы"
	DefaultVoiceChannel  = "Общий"
)

// CreateDefaultChannels creates, inside tx, a text category with a text
// channel and a voice category with a voice channel.
func (b *Bootstrapper) CreateDefaultChannels(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error {
	now := b.now()
	for i, d := range []struct {
		category, channel string
		typ               Type
	}{
		{DefaultTextCategory, DefaultTextChannel, TypeText},
		{DefaultVoiceCategory, DefaultVoiceChannel, TypeVoice},
	} {
		cat, err := b.q.CreateChannel(ctx, tx, store.CreateChannelParams{ID: b.ids.Next(), GuildID: guildID,
			Type: int16(TypeCategory), Name: d.category, Position: int32(i), CreatedAt: now}) //nolint:gosec // G115: 0 or 1
		if err != nil {
			return fmt.Errorf("channels: create category: %w", err)
		}
		if _, err = b.q.CreateChannel(ctx, tx, store.CreateChannelParams{ID: b.ids.Next(), GuildID: guildID,
			Type: int16(d.typ), Name: d.channel, ParentID: &cat.ID, PermissionSynced: true, CreatedAt: now}); err != nil {
			return fmt.Errorf("channels: create channel: %w", err)
		}
	}
	return nil
}

func (s *Service) publish(ctx context.Context, e events.Event) {
	if s.pub != nil {
		s.pub.Publish(ctx, e)
	}
}

func (s *Service) publishSync(ctx context.Context, guildID snowflake.ID) {
	s.publish(ctx, events.Event{Type: events.GuildSync,
		Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID},
		Payload:  guilds.GuildSyncPayload{GuildID: guildID}})
}

// publishChannel announces c to the members who can view it when the
// gateway fans the event out (after the change).
func (s *Service) publishChannel(ctx context.Context, typ string, c Channel) {
	s.publish(ctx, events.Event{Type: typ,
		Audience: events.Audience{Kind: events.AudienceChannel, GuildID: c.GuildID, ChannelID: c.ID},
		Payload:  c.Public()})
}

// read runs fn in a read-only repeatable-read transaction, so the channel,
// overwrite and guild reads see one snapshot.
func (s *Service) read(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

// --- conversions ---

func toChannel(c store.Channel) Channel {
	return Channel{ID: c.ID, GuildID: c.GuildID, Type: Type(c.Type), Name: c.Name, Topic: c.Topic,
		LastMessageID: c.LastMessageID, ParentID: c.ParentID, Position: int(c.Position), PermissionSynced: c.PermissionSynced}
}

func toOverwrite(o store.Overwrite) permissions.Overwrite {
	return permissions.Overwrite{ID: o.TargetID, Type: permissions.OverwriteType(o.TargetType),
		Allow: permissions.Permission(o.Allow), Deny: permissions.Permission(o.Deny)} //nolint:gosec // G115: stored masks are within All
}

func mask(p permissions.Permission) int64 {
	return int64(p) //nolint:gosec // G115: masks are checked against permissions.All, which fits
}

func cloneOverwrites(ows []permissions.Overwrite) []permissions.Overwrite {
	return append([]permissions.Overwrite{}, ows...)
}

// --- validation ---

func normalizeName(s string) (string, error) {
	name, problem := textx.Name(s, 1, 100)
	if problem != "" {
		return "", invalid("name", problem)
	}
	return name, nil
}

// normalizeTopic trims a topic; nil, empty or blank-only-spaces clear it.
// It may span lines but holds no other control or bidi control characters
// and must show something.
func normalizeTopic(t *string) (*string, error) {
	if t == nil {
		return nil, nil
	}
	s := strings.TrimFunc(*t, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	var msg string
	switch {
	case textx.ContainsBidiControl(*t):
		msg = "must not contain bidirectional control characters"
	case s == "":
		return nil, nil
	case utf8.RuneCountInString(s) > MaxTopic:
		msg = fmt.Sprintf("must be at most %d characters", MaxTopic)
	case strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }):
		msg = "must not contain control characters other than line breaks"
	case !textx.HasVisible(s):
		msg = "must contain a visible character"
	default:
		return &s, nil
	}
	return nil, invalid("topic", msg)
}

// --- the channels of a guild, in memory ---

// node is a channel with its own (not effective) overwrites and the place
// it had when loaded.
type node struct {
	ch Channel
	// was is the stored parent (0: none), position and sync flag.
	wasParent   snowflake.ID
	wasPosition int
	wasSynced   bool
	// owDirty: ch.Overwrites must be written back.
	owDirty bool
}

func (n *node) parent() snowflake.ID {
	if n.ch.ParentID == nil {
		return 0
	}
	return *n.ch.ParentID
}

func (n *node) setParent(p snowflake.ID) {
	if p == 0 {
		n.ch.ParentID = nil
		return
	}
	n.ch.ParentID = &p
}

func (n *node) moved() bool {
	return n.parent() != n.wasParent || n.ch.Position != n.wasPosition || n.ch.PermissionSynced != n.wasSynced
}

// layout is every channel of a guild with its overwrites.
type layout struct {
	guildID snowflake.ID
	nodes   map[snowflake.ID]*node
}

// loadLayout reads the channels and overwrites of guildID in two queries.
func loadLayout(ctx context.Context, q *store.Queries, db store.DBTX, guildID snowflake.ID) (*layout, error) {
	rows, err := q.GuildChannels(ctx, db, guildID)
	if err != nil {
		return nil, fmt.Errorf("channels: list channels: %w", err)
	}
	ows, err := q.GuildOverwrites(ctx, db, guildID)
	if err != nil {
		return nil, fmt.Errorf("channels: list overwrites: %w", err)
	}
	l := &layout{guildID: guildID, nodes: make(map[snowflake.ID]*node, len(rows))}
	for _, r := range rows {
		n := &node{ch: toChannel(r), wasPosition: int(r.Position), wasSynced: r.PermissionSynced}
		n.ch.Overwrites = []permissions.Overwrite{}
		n.wasParent = n.parent()
		l.nodes[r.ID] = n
	}
	for _, o := range ows {
		if n := l.nodes[o.ChannelID]; n != nil {
			n.ch.Overwrites = append(n.ch.Overwrites, toOverwrite(o))
		}
	}
	return l, nil
}

// effective returns the overwrites that apply in n.
func (l *layout) effective(n *node) []permissions.Overwrite {
	if n.ch.PermissionSynced && n.ch.ParentID != nil {
		if p := l.nodes[*n.ch.ParentID]; p != nil {
			return p.ch.Overwrites
		}
	}
	return n.ch.Overwrites
}

// view returns n as seen from outside: with its effective overwrites.
func (l *layout) view(n *node) Channel {
	c := n.ch
	c.Overwrites = cloneOverwrites(l.effective(n))
	return c
}

func (l *layout) canView(sub permissions.Subject, n *node) bool {
	return permissions.InChannel(sub, l.effective(n)).Has(permissions.ViewChannel)
}

// scopes returns the channels of each parent (0: top level) by position.
func (l *layout) scopes() map[snowflake.ID][]*node {
	out := make(map[snowflake.ID][]*node)
	for _, n := range l.nodes {
		out[n.parent()] = append(out[n.parent()], n)
	}
	for _, sc := range out {
		sortByPosition(sc)
	}
	return out
}

func sortByPosition(ns []*node) {
	slices.SortFunc(ns, func(a, b *node) int {
		if a.ch.Position != b.ch.Position {
			return a.ch.Position - b.ch.Position
		}
		return cmp.Compare(a.ch.ID, b.ch.ID)
	})
}

// place assigns parents and dense positions from scopes. A synced channel
// that changes parent (to another category or the top level) keeps the
// overwrites it had as its own and stops being synced, so a move never
// changes who can see it; only an explicit sync does.
func (l *layout) place(scopes map[snowflake.ID][]*node) {
	for parent, sc := range scopes {
		for i, n := range sc {
			if parent != n.parent() && n.ch.PermissionSynced {
				n.ch.Overwrites = cloneOverwrites(l.effective(n))
				n.ch.PermissionSynced = false
				n.owDirty = true
			}
			n.setParent(parent)
			n.ch.Position = i
		}
	}
}

// sorted returns the channels in display order: the top level by position,
// each category followed by its channels.
func (l *layout) sorted() []*node {
	scopes := l.scopes()
	out := make([]*node, 0, len(l.nodes))
	for _, n := range scopes[0] {
		out = append(out, n)
		if n.ch.Type == TypeCategory {
			out = append(out, scopes[n.ch.ID]...)
		}
	}
	if len(out) < len(l.nodes) { // children of a missing parent; cannot happen
		seen := make(map[snowflake.ID]bool, len(out))
		for _, n := range out {
			seen[n.ch.ID] = true
		}
		for _, n := range l.nodes {
			if !seen[n.ch.ID] {
				out = append(out, n)
			}
		}
	}
	return out
}

// save writes the moved channels and dirty overwrites back through tx and
// returns the channels that changed, in display order.
func (s *Service) save(ctx context.Context, tx pgx.Tx, l *layout) ([]Channel, error) {
	var p store.SetChannelLayoutParams
	p.GuildID = l.guildID
	var changed []Channel
	for _, n := range l.sorted() {
		if n.owDirty {
			if err := s.writeOverwrites(ctx, tx, n.ch.ID, n.ch.Overwrites); err != nil {
				return nil, err
			}
		}
		if !n.moved() && !n.owDirty {
			continue
		}
		if n.moved() {
			p.Ids = append(p.Ids, int64(n.ch.ID))
			p.Positions = append(p.Positions, int32(n.ch.Position)) //nolint:gosec // G115: at most MaxChannels
			p.ParentIds = append(p.ParentIds, int64(n.parent()))
			p.Synced = append(p.Synced, n.ch.PermissionSynced)
		}
		changed = append(changed, l.view(n))
	}
	if len(p.Ids) > 0 {
		if err := s.q.SetChannelLayout(ctx, tx, p); err != nil {
			return nil, fmt.Errorf("channels: move channels: %w", err)
		}
	}
	return changed, nil
}

func (s *Service) writeOverwrites(ctx context.Context, tx pgx.Tx, channelID snowflake.ID, ows []permissions.Overwrite) error {
	if err := s.q.ClearOverwrites(ctx, tx, channelID); err != nil {
		return fmt.Errorf("channels: clear overwrites: %w", err)
	}
	if len(ows) == 0 {
		return nil
	}
	p := store.InsertOverwritesParams{ChannelID: channelID}
	for _, o := range ows {
		p.TargetIds = append(p.TargetIds, int64(o.ID))
		p.TargetTypes = append(p.TargetTypes, int16(o.Type)) //nolint:gosec // G115: 0 or 1
		p.Allows = append(p.Allows, mask(o.Allow))
		p.Denies = append(p.Denies, mask(o.Deny))
	}
	if err := s.q.InsertOverwrites(ctx, tx, p); err != nil {
		return fmt.Errorf("channels: write overwrites: %w", err)
	}
	return nil
}
