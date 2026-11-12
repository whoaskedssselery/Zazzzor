// Package notifysettings owns each user's personal notification settings:
// a mute and an alert level per server and for their DMs, and the same per
// channel. They change no unread state; the client applies them.
package notifysettings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/notifysettings/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Alert levels, as Discord numbers them. Inherit exists for a channel only.
const (
	AllMessages  int16 = 0
	OnlyMentions int16 = 1
	Nothing      int16 = 2
	Inherit      int16 = 3
)

const (
	// MaxOverrides is the most channel overrides one scope holds.
	MaxOverrides = 500
	// MaxMute is how far ahead a timed mute may end.
	MaxMute = 30 * 24 * time.Hour
	// EventUpdate goes to the user's own sessions after every change.
	EventUpdate = "USER_GUILD_SETTINGS_UPDATE"
)

// Override is the settings of one channel.
type Override struct {
	ChannelID            snowflake.ID `json:"channel_id"`
	Muted                bool         `json:"muted"`
	MuteUntil            *string      `json:"mute_until"`
	MessageNotifications int16        `json:"message_notifications"`
}

// Settings is the schema UserGuildSettings: one server's, or the DMs' (GuildID nil).
type Settings struct {
	GuildID              *snowflake.ID `json:"guild_id"`
	Version              int64         `json:"version"`
	Muted                bool          `json:"muted"`
	MuteUntil            *string       `json:"mute_until"`
	MessageNotifications int16         `json:"message_notifications"`
	SuppressEveryone     bool          `json:"suppress_everyone"`
	ChannelOverrides     []Override    `json:"channel_overrides"`
}

// ChannelScope tells which server a channel the user may view is in (0 for
// a DM or group they are in), else channels.ErrNotFound.
type ChannelScope interface {
	Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error)
}

// Options configures a Service. Pool and Channels are required.
type Options struct {
	Pool      *pgxpool.Pool
	Channels  ChannelScope
	Publisher events.Publisher
	Now       func() time.Time
}

// Service reads and changes notification settings.
type Service struct {
	pool     *pgxpool.Pool
	q        *store.Queries
	channels ChannelScope
	pub      events.Publisher
	now      func() time.Time
	limiter  *ratelimit.Limiter
}

// NewService builds a Service from o; PATCHes are limited to 30 a minute per user.
func NewService(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{pool: o.Pool, q: store.New(), channels: o.Channels, pub: o.Publisher, now: o.Now,
		limiter: ratelimit.New(rate.Every(2*time.Second), 30, time.Hour, ratelimit.WithClock(o.Now))}
}

// defaults are the settings of a scope nobody changed: only mentions in a
// server, every message in the DMs (Discord's defaults).
func defaults(guildID *snowflake.ID) Settings {
	level := AllMessages
	if guildID != nil {
		level = OnlyMentions
	}
	return Settings{GuildID: guildID, MessageNotifications: level, ChannelOverrides: []Override{}}
}

func scopeOf(guildID *snowflake.ID) int64 {
	if guildID == nil {
		return 0
	}
	return int64(*guildID)
}

func dbGuild(guildID *snowflake.ID) pgtype.Int8 {
	return pgtype.Int8{Int64: scopeOf(guildID), Valid: guildID != nil}
}

func timeText(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := httpx.FormatTime(*t)
	return &s
}

// load reads one scope's settings, the defaults where nothing is stored.
func (s *Service) load(ctx context.Context, q store.DBTX, user snowflake.ID, guildID *snowflake.ID) (Settings, error) {
	out := defaults(guildID)
	row, err := s.q.ScopeSettings(ctx, q, store.ScopeSettingsParams{UserID: int64(user), ScopeID: scopeOf(guildID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Settings{}, fmt.Errorf("notifysettings: read scope: %w", err)
	default:
		out.Muted, out.MuteUntil = row.Muted, timeText(row.MuteUntil)
		out.MessageNotifications, out.SuppressEveryone = row.MessageNotifications, row.SuppressEveryone
	}
	rows, err := s.q.ScopeOverrides(ctx, q, store.ScopeOverridesParams{UserID: int64(user), GuildID: dbGuild(guildID)})
	if err != nil {
		return Settings{}, fmt.Errorf("notifysettings: read overrides: %w", err)
	}
	for _, r := range rows {
		out.ChannelOverrides = append(out.ChannelOverrides, Override{ChannelID: snowflake.ID(r.ChannelID),
			Muted: r.Muted, MuteUntil: timeText(r.MuteUntil), MessageNotifications: r.MessageNotifications})
	}
	return out, nil
}

// List returns every scope of user whose settings differ from the
// defaults, for READY.
func (s *Service) List(ctx context.Context, user snowflake.ID) ([]Settings, error) {
	var out []Settings
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = s.listSnapshot(ctx, tx, user)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("notifysettings: list snapshot: %w", err)
	}
	return out, nil
}

// listSnapshot reads the revision and all scopes at the same point in time.
func (s *Service) listSnapshot(ctx context.Context, tx pgx.Tx, user snowflake.ID) ([]Settings, error) {
	version, err := s.q.UserSettingsVersion(ctx, tx, int64(user))
	if err != nil {
		return nil, fmt.Errorf("notifysettings: read version: %w", err)
	}
	scopes, err := s.q.AllScopeSettings(ctx, tx, int64(user))
	if err != nil {
		return nil, fmt.Errorf("notifysettings: list scopes: %w", err)
	}
	overrides, err := s.q.AllOverrides(ctx, tx, int64(user))
	if err != nil {
		return nil, fmt.Errorf("notifysettings: list overrides: %w", err)
	}
	byScope := map[int64]*Settings{}
	var order []int64
	get := func(g pgtype.Int8) *Settings {
		key := g.Int64
		if st, ok := byScope[key]; ok {
			return st
		}
		var guildID *snowflake.ID
		if g.Valid {
			id := snowflake.ID(g.Int64)
			guildID = &id
		}
		st := defaults(guildID)
		st.Version = version
		byScope[key] = &st
		order = append(order, key)
		return &st
	}
	for _, r := range scopes {
		st := get(r.GuildID)
		st.Muted, st.MuteUntil = r.Muted, timeText(r.MuteUntil)
		st.MessageNotifications, st.SuppressEveryone = r.MessageNotifications, r.SuppressEveryone
	}
	for _, r := range overrides {
		st := get(r.GuildID)
		st.ChannelOverrides = append(st.ChannelOverrides, Override{ChannelID: snowflake.ID(r.ChannelID),
			Muted: r.Muted, MuteUntil: timeText(r.MuteUntil), MessageNotifications: r.MessageNotifications})
	}
	out := make([]Settings, 0, len(order))
	for _, key := range order {
		out = append(out, *byScope[key])
	}
	return out, nil
}

// Patch is a PATCH body; a nil field is left as it is.
type Patch struct {
	Muted                *bool
	MuteUntil            Optional[time.Time]
	MessageNotifications *int16
	SuppressEveryone     *bool
	ChannelOverrides     map[snowflake.ID]OverridePatch
}

// OverridePatch is one channel of a PATCH.
type OverridePatch struct {
	Muted                *bool
	MuteUntil            Optional[time.Time]
	MessageNotifications *int16
}

// mute is a mute after a change: `muted` alone unmutes (and drops the
// end) or mutes until turned off; an end needs the mute on.
func (s *Service) mute(field string, muted bool, until *string, pMuted *bool, pUntil Optional[time.Time]) (bool, *time.Time, error) {
	var end *time.Time
	if until != nil {
		if t, err := time.Parse(time.RFC3339Nano, *until); err == nil {
			end = &t
		}
	}
	if pMuted != nil {
		muted = *pMuted
		end = nil
	}
	if pUntil.Set {
		end = pUntil.Value
		if end != nil {
			now := s.now()
			if !end.After(now) || end.Sub(now) > MaxMute {
				return false, nil, invalid(field+"mute_until", "must be in the future and at most 30 days ahead")
			}
		}
	}
	if end != nil && !muted {
		return false, nil, invalid(field+"mute_until", "needs muted")
	}
	return muted, end, nil
}

func invalid(field, message string) error {
	return httpx.BadRequest("validation_failed", "some fields are invalid", httpx.FieldError{Field: field, Message: message})
}

// Update applies p to user's settings of guildID (nil: the DMs) and returns
// them whole; the user's own sessions get USER_GUILD_SETTINGS_UPDATE.
func (s *Service) Update(ctx context.Context, user snowflake.ID, guildID *snowflake.ID, p Patch) (Settings, error) {
	if ok, retry := s.limiter.Allow(user.String()); !ok {
		return Settings{}, httpx.TooManyRequests(retry)
	}
	if guildID != nil {
		member, err := s.q.IsMember(ctx, s.pool, store.IsMemberParams{GuildID: int64(*guildID), UserID: int64(user)})
		if err != nil {
			return Settings{}, fmt.Errorf("notifysettings: check membership: %w", err)
		}
		if !member {
			return Settings{}, httpx.NotFound()
		}
	} else if p.SuppressEveryone != nil {
		return Settings{}, invalid("suppress_everyone", "applies only to a server")
	}
	if p.MessageNotifications != nil && (*p.MessageNotifications < AllMessages || *p.MessageNotifications > Nothing) {
		return Settings{}, invalid("message_notifications", "must be 0, 1 or 2")
	}
	// Each channel must be in this scope and visible: checked before the
	// transaction, the resolver has its own.
	for id, o := range p.ChannelOverrides {
		if o.MessageNotifications != nil && (*o.MessageNotifications < AllMessages || *o.MessageNotifications > Inherit) {
			return Settings{}, invalid("channel_overrides", "message_notifications must be 0 to 3")
		}
		_, scope, err := s.channels.Permissions(ctx, user, id)
		if errors.Is(err, channels.ErrNotFound) || (err == nil && int64(scope) != scopeOf(guildID)) {
			return Settings{}, httpx.BadRequest("unknown_channel", "a channel is not in this scope or not visible")
		}
		if err != nil {
			return Settings{}, fmt.Errorf("notifysettings: check channel: %w", err)
		}
	}

	var out Settings
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.q.LockUserSettings(ctx, tx, int64(user)); err != nil {
			return fmt.Errorf("notifysettings: lock: %w", err)
		}
		cur, err := s.load(ctx, tx, user, guildID)
		if err != nil {
			return err
		}
		if err = s.saveScope(ctx, tx, user, guildID, cur, p); err != nil {
			return err
		}
		held := map[snowflake.ID]Override{}
		for _, o := range cur.ChannelOverrides {
			held[o.ChannelID] = o
		}
		for id, op := range p.ChannelOverrides {
			if err = s.saveOverride(ctx, tx, user, guildID, id, held, op); err != nil {
				return err
			}
		}
		out, err = s.load(ctx, tx, user, guildID)
		if err != nil {
			return err
		}
		if len(out.ChannelOverrides) > MaxOverrides {
			return httpx.BadRequest("too_many_overrides", fmt.Sprintf("at most %d channel overrides", MaxOverrides))
		}
		out.Version, err = s.q.AdvanceUserSettingsVersion(ctx, tx, int64(user))
		if err != nil {
			return fmt.Errorf("notifysettings: advance version: %w", err)
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	if s.pub != nil {
		s.pub.Publish(ctx, events.Event{Type: EventUpdate,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{user}}, Payload: out})
	}
	return out, nil
}

func (s *Service) saveScope(ctx context.Context, tx pgx.Tx, user snowflake.ID, guildID *snowflake.ID, cur Settings, p Patch) error {
	muted, end, err := s.mute("", cur.Muted, cur.MuteUntil, p.Muted, p.MuteUntil)
	if err != nil {
		return err
	}
	level := cur.MessageNotifications
	if p.MessageNotifications != nil {
		level = *p.MessageNotifications
	}
	suppress := cur.SuppressEveryone
	if p.SuppressEveryone != nil {
		suppress = *p.SuppressEveryone
	}
	def := defaults(guildID)
	key := store.DeleteScopeSettingsParams{UserID: int64(user), ScopeID: scopeOf(guildID)}
	if !muted && level == def.MessageNotifications && !suppress {
		if err = s.q.DeleteScopeSettings(ctx, tx, key); err != nil {
			return fmt.Errorf("notifysettings: clear scope: %w", err)
		}
		return nil
	}
	if err = s.q.SaveScopeSettings(ctx, tx, store.SaveScopeSettingsParams{UserID: int64(user), GuildID: dbGuild(guildID),
		Muted: muted, MuteUntil: end, MessageNotifications: level, SuppressEveryone: suppress}); err != nil {
		return fmt.Errorf("notifysettings: save scope: %w", err)
	}
	return nil
}

func (s *Service) saveOverride(ctx context.Context, tx pgx.Tx, user snowflake.ID, guildID *snowflake.ID,
	id snowflake.ID, held map[snowflake.ID]Override, op OverridePatch,
) error {
	cur, ok := held[id]
	if !ok {
		cur = Override{ChannelID: id, MessageNotifications: Inherit}
	}
	muted, end, err := s.mute("channel_overrides.", cur.Muted, cur.MuteUntil, op.Muted, op.MuteUntil)
	if err != nil {
		return err
	}
	level := cur.MessageNotifications
	if op.MessageNotifications != nil {
		level = *op.MessageNotifications
	}
	if !muted && level == Inherit {
		if err = s.q.DeleteOverride(ctx, tx, store.DeleteOverrideParams{UserID: int64(user), ChannelID: int64(id)}); err != nil {
			return fmt.Errorf("notifysettings: clear override: %w", err)
		}
		return nil
	}
	err = s.q.SaveOverride(ctx, tx, store.SaveOverrideParams{UserID: int64(user), ChannelID: int64(id),
		GuildID: dbGuild(guildID), Muted: muted, MuteUntil: end, MessageNotifications: level})
	if err != nil {
		return fmt.Errorf("notifysettings: save override: %w", err)
	}
	return nil
}
