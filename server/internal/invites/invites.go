// Package invites owns the invite links of the guilds: creating them,
// previewing them and joining a guild through one.
package invites

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/invites/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Limits and defaults.
const (
	// CodeLength is the length of an invite code.
	CodeLength = 8
	// DefaultMaxAge is how long an invite lasts unless told otherwise.
	DefaultMaxAge = 7 * 24 * time.Hour
	// MaxMaxAge is the longest a limited invite may last; 0 means forever.
	MaxMaxAge = 30 * 24 * time.Hour
	// MaxMaxUses is the highest use limit; 0 means unlimited.
	MaxMaxUses = 100
	// MaxListed is how many invites GET /guilds/{id}/invites returns at most.
	MaxListed = 1000
	// codeAttempts is how many fresh codes creation tries on a collision.
	codeAttempts = 5
)

// alphabet is base62.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Rate limits per user.
var (
	// Creations: 20 per hour.
	createRate  = rate.Every(time.Hour / 20)
	createBurst = 20
	createTTL   = 2 * time.Hour
	// Lookups of a code (preview, join, delete): 30 per minute, so codes
	// cannot be enumerated.
	lookupRate  = rate.Every(time.Minute / 30)
	lookupBurst = 30
	lookupTTL   = 2 * time.Minute
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	// ErrNotFound: no such invite (or a malformed code), or a guild or
	// channel the user may not see.
	ErrNotFound    = errors.New("invites: not found")
	ErrExpired     = errors.New("invites: expired")
	ErrExhausted   = errors.New("invites: used up")
	ErrMissingPerm = errors.New("invites: missing permission")
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
	return "invites: invalid " + strings.Join(names, ", ")
}

func invalid(field, msg string) error {
	return &ValidationError{Fields: []httpx.FieldError{{Field: field, Message: msg}}}
}

// ToProblem maps the errors of this package (and of channels and guilds)
// to their HTTP problem; any other error is returned unchanged.
func ToProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		return httpx.BadRequest("validation_failed", "some fields are invalid", verr.Fields...)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound()
	case errors.Is(err, ErrExpired):
		return httpx.Gone("invite_expired")
	case errors.Is(err, ErrExhausted):
		return httpx.Gone("invite_exhausted")
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	}
	return channels.ToProblem(err)
}

// Invite is an invite with what its preview shows.
type Invite struct {
	Code      string
	Guild     guilds.Guild
	Channel   channels.Channel
	Inviter   auth.User
	Uses      int
	MaxUses   int
	ExpiresAt *time.Time
}

// PublicInviteGuild is the guild of an invite as JSON.
type PublicInviteGuild struct {
	ID      snowflake.ID `json:"id"`
	Name    string       `json:"name"`
	IconURL *string      `json:"icon_url"`
}

// PublicInviteChannel is the channel of an invite as JSON.
type PublicInviteChannel struct {
	ID   snowflake.ID `json:"id"`
	Name string       `json:"name"`
}

// PublicInvite is the JSON form of an Invite (schema Invite).
type PublicInvite struct {
	Code      string              `json:"code"`
	Guild     PublicInviteGuild   `json:"guild"`
	Channel   PublicInviteChannel `json:"channel"`
	Inviter   auth.PublicUser     `json:"inviter"`
	Uses      int                 `json:"uses"`
	MaxUses   int                 `json:"max_uses"`
	ExpiresAt *string             `json:"expires_at"`
}

// Public returns the JSON form of i.
func (i Invite) Public() PublicInvite {
	g := i.Guild.Public()
	p := PublicInvite{Code: i.Code,
		Guild:   PublicInviteGuild{ID: g.ID, Name: g.Name, IconURL: g.IconURL},
		Channel: PublicInviteChannel{ID: i.Channel.ID, Name: i.Channel.Name},
		Inviter: i.Inviter.Public(), Uses: i.Uses, MaxUses: i.MaxUses}
	if i.ExpiresAt != nil {
		t := httpx.FormatTime(*i.ExpiresAt)
		p.ExpiresAt = &t
	}
	return p
}

// Channels is what invites needs from the channels module.
type Channels interface {
	Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error)
	VisibleChannels(ctx context.Context, userID, guildID snowflake.ID) ([]channels.Channel, error)
	Lookup(ctx context.Context, channelID snowflake.ID) (channels.Channel, error)
}

var _ Channels = (*channels.Service)(nil)

// Guilds is what invites needs from the guilds module.
type Guilds interface {
	Subject(ctx context.Context, guildID, userID snowflake.ID) (permissions.Subject, error)
	Lookup(ctx context.Context, guildID snowflake.ID) (guilds.Guild, error)
	AddMember(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID) (bool, error)
	AnnounceJoin(ctx context.Context, guildID, userID snowflake.ID)
}

var _ Guilds = (*guilds.Service)(nil)

// Options configures a Service. Pool, Channels, Guilds and Users are
// required.
type Options struct {
	Pool     *pgxpool.Pool
	Channels Channels
	Guilds   Guilds
	Users    guilds.Users
	// CreateLimiter limits invite creations per user; nil takes the default
	// (20 per hour).
	CreateLimiter *ratelimit.Limiter
	// LookupLimiter limits previews, joins and deletions by code per user;
	// nil takes the default (30 per minute).
	LookupLimiter *ratelimit.Limiter
}

// Service implements invites.
type Service struct {
	pool          *pgxpool.Pool
	q             *store.Queries
	channels      Channels
	guilds        Guilds
	users         guilds.Users
	createLimiter *ratelimit.Limiter
	lookupLimiter *ratelimit.Limiter
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: store.New(), channels: o.Channels, guilds: o.Guilds, users: o.Users,
		createLimiter: o.CreateLimiter, lookupLimiter: o.LookupLimiter}
	if s.createLimiter == nil {
		s.createLimiter = ratelimit.New(createRate, createBurst, createTTL)
	}
	if s.lookupLimiter == nil {
		s.lookupLimiter = ratelimit.New(lookupRate, lookupBurst, lookupTTL)
	}
	return s
}

// NewCode returns a random base62 code of CodeLength characters from
// crypto/rand, without modulo bias.
func NewCode() string {
	var out [CodeLength]byte
	var buf [2 * CodeLength]byte
	n := 0
	for n < CodeLength {
		_, _ = rand.Read(buf[:]) // never fails (crypto/rand panics instead)
		for _, b := range buf {
			// 248 = 4 * 62: the bytes above it would favour the first symbols.
			if b < 248 && n < CodeLength {
				out[n] = alphabet[b%62]
				n++
			}
		}
	}
	return string(out[:])
}

// ValidCode reports whether s has the form of an invite code.
func ValidCode(s string) bool {
	if len(s) != CodeLength {
		return false
	}
	for i := range len(s) {
		if !strings.ContainsRune(alphabet, rune(s[i])) {
			return false
		}
	}
	return true
}

// CreateFields are the settings of a new invite; nil takes the default.
type CreateFields struct {
	// MaxAgeSeconds 0 means the invite never expires.
	MaxAgeSeconds *int
	// MaxUses 0 means unlimited.
	MaxUses *int
}

// Create creates an invite to the channel; the actor needs CREATE_INVITE
// in it. Each call counts against the per-user creation limit.
func (s *Service) Create(ctx context.Context, channelID snowflake.ID, actor auth.User, f CreateFields) (Invite, error) {
	maxAge := int(DefaultMaxAge / time.Second)
	if f.MaxAgeSeconds != nil {
		maxAge = *f.MaxAgeSeconds
	}
	if maxAge < 0 || maxAge > int(MaxMaxAge/time.Second) {
		return Invite{}, invalid("max_age_seconds", fmt.Sprintf("must be from 0 to %d", int(MaxMaxAge/time.Second)))
	}
	maxUses := 0
	if f.MaxUses != nil {
		maxUses = *f.MaxUses
	}
	if maxUses < 0 || maxUses > MaxMaxUses {
		return Invite{}, invalid("max_uses", fmt.Sprintf("must be from 0 to %d", MaxMaxUses))
	}
	perm, guildID, err := s.channels.Permissions(ctx, actor.ID, channelID)
	if err != nil {
		return Invite{}, err
	}
	if !perm.Has(permissions.CreateInvite) {
		return Invite{}, ErrMissingPerm
	}
	if ok, retry := s.createLimiter.Allow(actor.ID.String()); !ok {
		return Invite{}, &auth.RateLimitedError{RetryAfter: retry}
	}
	var row store.Invite
create:
	for attempt := 1; ; attempt++ {
		row, err = s.q.CreateInvite(ctx, s.pool, store.CreateInviteParams{Code: NewCode(), GuildID: guildID,
			ChannelID: channelID, InviterID: actor.ID,
			MaxUses: int32(maxUses), MaxAgeSeconds: int32(maxAge)}) //nolint:gosec // G115: range checked
		pgErr, isPg := errors.AsType[*pgconn.PgError](err)
		switch {
		case err == nil:
			break create
		case isPg && pgErr.Code == "23505" && attempt < codeAttempts:
			continue // the code is taken: draw another
		case isPg && pgErr.Code == "23503":
			return Invite{}, ErrNotFound // the channel went away meanwhile
		default:
			return Invite{}, fmt.Errorf("invites: create invite: %w", err)
		}
	}
	return s.complete(ctx, row)
}

// complete adds the guild, channel and inviter to a stored invite.
func (s *Service) complete(ctx context.Context, row store.Invite) (Invite, error) {
	g, err := s.guilds.Lookup(ctx, row.GuildID)
	if errors.Is(err, guilds.ErrNotFound) {
		return Invite{}, ErrNotFound
	}
	if err != nil {
		return Invite{}, err
	}
	c, err := s.channels.Lookup(ctx, row.ChannelID)
	if err != nil {
		return Invite{}, err
	}
	users, err := s.users.UsersByIDs(ctx, []snowflake.ID{row.InviterID})
	if err != nil {
		return Invite{}, err
	}
	if len(users) != 1 {
		return Invite{}, fmt.Errorf("invites: inviter %s is missing", row.InviterID)
	}
	return Invite{Code: row.Code, Guild: g, Channel: c, Inviter: users[0], Uses: int(row.Uses),
		MaxUses: int(row.MaxUses), ExpiresAt: row.ExpiresAt}, nil
}

func toInvite(r store.InviteByCodeRow) store.Invite {
	return store.Invite{Code: r.Code, GuildID: r.GuildID, ChannelID: r.ChannelID, InviterID: r.InviterID,
		MaxUses: r.MaxUses, Uses: r.Uses, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt}
}

func (s *Service) byCode(ctx context.Context, db store.DBTX, code string) (store.InviteByCodeRow, error) {
	if !ValidCode(code) {
		return store.InviteByCodeRow{}, ErrNotFound
	}
	row, err := s.q.InviteByCode(ctx, db, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.InviteByCodeRow{}, ErrNotFound
	}
	if err != nil {
		return store.InviteByCodeRow{}, fmt.Errorf("invites: load invite: %w", err)
	}
	return row, nil
}

// Preview returns the invite behind code for anyone signed in; an expired
// or used-up one is ErrExpired or ErrExhausted.
func (s *Service) Preview(ctx context.Context, code string) (Invite, error) {
	row, err := s.byCode(ctx, s.pool, code)
	if err != nil {
		return Invite{}, err
	}
	switch {
	case row.Expired:
		return Invite{}, ErrExpired
	case row.Exhausted:
		return Invite{}, ErrExhausted
	}
	return s.complete(ctx, toInvite(row))
}

// Join makes userID a member of the invite's guild and returns the guild.
// The membership and the use are written in one transaction, the use only
// if the invite is still valid then, so concurrent joins never exceed
// max_uses. A member already in the guild gets it back without using the
// invite up, whatever its state.
func (s *Service) Join(ctx context.Context, code string, userID snowflake.ID) (guilds.Guild, error) {
	var (
		guildID snowflake.ID
		added   bool
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		added = false
		row, err := s.byCode(ctx, tx, code)
		if err != nil {
			return err
		}
		guildID = row.GuildID
		if added, err = s.guilds.AddMember(ctx, tx, row.GuildID, userID); err != nil {
			if errors.Is(err, guilds.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		if !added {
			return nil
		}
		_, err = s.q.UseInvite(ctx, tx, code)
		if errors.Is(err, pgx.ErrNoRows) {
			return s.whyUnusable(ctx, tx, code)
		}
		if err != nil {
			return fmt.Errorf("invites: use invite: %w", err)
		}
		return nil
	})
	if err != nil {
		return guilds.Guild{}, err
	}
	if added {
		s.guilds.AnnounceJoin(ctx, guildID, userID)
	}
	g, err := s.guilds.Lookup(ctx, guildID)
	if errors.Is(err, guilds.ErrNotFound) {
		return guilds.Guild{}, ErrNotFound
	}
	return g, err
}

// whyUnusable tells why UseInvite took no use, reading the latest state.
func (s *Service) whyUnusable(ctx context.Context, tx pgx.Tx, code string) error {
	row, err := s.byCode(ctx, tx, code)
	switch {
	case err != nil:
		return err // deleted meanwhile: ErrNotFound
	case row.Expired:
		return ErrExpired
	default:
		return ErrExhausted
	}
}

// List returns the invites of the guild (newest first, at most MaxListed)
// to channels the actor can see; the actor needs MANAGE_GUILD.
func (s *Service) List(ctx context.Context, guildID, actorID snowflake.ID) ([]Invite, error) {
	sub, err := s.guilds.Subject(ctx, guildID, actorID)
	if err != nil {
		return nil, err
	}
	if !permissions.Base(sub).Has(permissions.ManageGuild) {
		return nil, ErrMissingPerm
	}
	visible, err := s.channels.VisibleChannels(ctx, actorID, guildID)
	if err != nil {
		return nil, err
	}
	byID := make(map[snowflake.ID]channels.Channel, len(visible))
	for _, c := range visible {
		byID[c.ID] = c
	}
	g, err := s.guilds.Lookup(ctx, guildID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.GuildInvites(ctx, s.pool, store.GuildInvitesParams{GuildID: guildID, Lim: MaxListed})
	if err != nil {
		return nil, fmt.Errorf("invites: list invites: %w", err)
	}
	var inviterIDs []snowflake.ID
	for _, r := range rows {
		inviterIDs = append(inviterIDs, r.InviterID)
	}
	users, err := s.users.UsersByIDs(ctx, inviterIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[snowflake.ID]auth.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	out := []Invite{}
	for _, r := range rows {
		c, seen := byID[r.ChannelID]
		u, known := userByID[r.InviterID]
		if !seen || !known {
			continue
		}
		out = append(out, Invite{Code: r.Code, Guild: g, Channel: c, Inviter: u, Uses: int(r.Uses),
			MaxUses: int(r.MaxUses), ExpiresAt: r.ExpiresAt})
	}
	return out, nil
}

// Delete deletes the invite; its creator or a member with MANAGE_GUILD
// may. To anyone outside the guild it does not exist.
func (s *Service) Delete(ctx context.Context, code string, actorID snowflake.ID) error {
	row, err := s.byCode(ctx, s.pool, code)
	if err != nil {
		return err
	}
	sub, err := s.guilds.Subject(ctx, row.GuildID, actorID)
	if errors.Is(err, guilds.ErrNotMember) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.InviterID != actorID && !permissions.Base(sub).Has(permissions.ManageGuild) {
		return ErrMissingPerm
	}
	if _, err = s.q.DeleteInvite(ctx, s.pool, code); err != nil {
		return fmt.Errorf("invites: delete invite: %w", err)
	}
	return nil
}
