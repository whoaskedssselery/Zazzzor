package guilds

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// Ban limits.
const (
	// MaxBanReason is the longest ban reason, in characters (runes).
	MaxBanReason = 512
	// MaxListedBans is how many bans GET /guilds/{id}/bans returns at most.
	MaxListedBans = 1000
)

// Errors of moderation.
var (
	// ErrBanned: the user is banned from the guild and cannot join it.
	ErrBanned = errors.New("guilds: banned")
	// ErrCannotTargetSelf: a moderator acting on themself (leaving is
	// DELETE /users/@me/guilds/{id}).
	ErrCannotTargetSelf = errors.New("guilds: cannot target self")
	// ErrHierarchy: the target is the owner or does not rank strictly below
	// the actor.
	ErrHierarchy = errors.New("guilds: role hierarchy")
)

// Ban is a ban of a guild.
type Ban struct {
	// User is nil when the account could not be read (it is being removed).
	User      *auth.User
	Reason    *string
	CreatedAt time.Time
}

// PublicBan is the JSON form of a Ban (schema Ban).
type PublicBan struct {
	User      *auth.PublicUser `json:"user"`
	Reason    *string          `json:"reason"`
	CreatedAt string           `json:"created_at"`
}

// Public returns the JSON form of b.
func (b Ban) Public() PublicBan {
	p := PublicBan{Reason: b.Reason, CreatedAt: httpx.FormatTime(b.CreatedAt)}
	if b.User != nil {
		u := b.User.Public()
		p.User = &u
	}
	return p
}

// BanPayload is the payload of GUILD_BAN_ADD and GUILD_BAN_REMOVE.
type BanPayload struct {
	GuildID snowflake.ID    `json:"guild_id"`
	User    auth.PublicUser `json:"user"`
}

// normalizeReason trims a ban reason; nil, empty or blank clear it. It
// follows the channel-topic rules for free text shown to moderators: it may
// span lines but holds no other control or bidi control characters, at
// most MaxBanReason characters, and must show something.
func normalizeReason(r *string) (*string, error) {
	if r == nil {
		return nil, nil
	}
	s := strings.TrimFunc(*r, func(c rune) bool { return unicode.IsSpace(c) || unicode.Is(unicode.Cf, c) })
	var msg string
	switch {
	case textx.ContainsBidiControl(*r):
		msg = "must not contain bidirectional control characters"
	case s == "":
		return nil, nil
	case utf8.RuneCountInString(s) > MaxBanReason:
		msg = fmt.Sprintf("must be at most %d characters", MaxBanReason)
	case strings.ContainsFunc(s, func(c rune) bool { return unicode.IsControl(c) && c != '\n' }):
		msg = "must not contain control characters other than line breaks"
	case !textx.HasVisible(s):
		msg = "must contain a visible character"
	default:
		return &s, nil
	}
	return nil, invalid("reason", msg)
}

// moderator checks, under the guild lock in tx, that actorID is a member
// holding need and is not targeting themself.
func (s *Service) moderator(ctx context.Context, tx pgx.Tx, guildID, actorID, targetID snowflake.ID,
	need permissions.Permission,
) (permissions.Subject, error) {
	if err := s.lockGuild(ctx, tx, guildID); err != nil {
		return permissions.Subject{}, err
	}
	actor, err := s.require(ctx, tx, guildID, actorID, need)
	if err != nil {
		return permissions.Subject{}, err
	}
	if targetID == actorID {
		return permissions.Subject{}, ErrCannotTargetSelf
	}
	return actor, nil
}

// account reads userID's account through tx; a missing one is ErrNotFound.
func (s *Service) account(ctx context.Context, tx pgx.Tx, userID snowflake.ID) (auth.User, error) {
	users, err := s.users.UsersByIDsIn(ctx, tx, []snowflake.ID{userID})
	if err != nil {
		return auth.User{}, err
	}
	if len(users) != 1 {
		return auth.User{}, ErrNotFound
	}
	return users[0], nil
}

// removeMember deletes the membership of userID with what hangs on it
// (their channel overwrites), inside tx holding the guild lock.
func (s *Service) removeMember(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID) error {
	if _, err := s.q.DeleteMember(ctx, tx, store.DeleteMemberParams{GuildID: guildID, UserID: userID}); err != nil {
		return fmt.Errorf("guilds: delete member: %w", err)
	}
	if err := s.overwrites.MemberRemoved(ctx, tx, guildID, userID); err != nil {
		return fmt.Errorf("guilds: drop member overwrites: %w", err)
	}
	return nil
}

// announceRemoval follows a committed removal of user: the remaining
// members get GUILD_MEMBER_REMOVE (the gateway then ends shared presences),
// the user GUILD_DELETE.
func (s *Service) announceRemoval(ctx context.Context, guildID snowflake.ID, user auth.User) {
	s.publish(ctx, events.Event{Type: events.GuildMemberRemove,
		Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guildID},
		Payload:  MemberRemovePayload{GuildID: guildID, User: user.Public()}})
	s.publish(ctx, events.Event{Type: events.GuildDelete,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{user.ID}},
		Payload:  GuildDeletePayload{ID: guildID}})
}

// publishBan announces a ban change to the members holding BAN_MEMBERS when
// the gateway sends it, never to the whole guild.
func (s *Service) publishBan(ctx context.Context, typ string, guildID snowflake.ID, user auth.User) {
	s.publish(ctx, events.Event{Type: typ,
		Audience: events.Audience{Kind: events.AudienceGuildPermission, GuildID: guildID, Permission: permissions.BanMembers},
		Payload:  BanPayload{GuildID: guildID, User: user.Public()}})
}

// Kick removes targetID from the guild; the actor needs KICK_MEMBERS and
// must outrank the target (CanActOn: never the owner, never oneself). The
// user may rejoin with an invite.
func (s *Service) Kick(ctx context.Context, guildID, targetID, actorID snowflake.ID) error {
	var target auth.User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		actor, err := s.moderator(ctx, tx, guildID, actorID, targetID, permissions.KickMembers)
		if err != nil {
			return err
		}
		sub, err := s.subject(ctx, tx, guildID, targetID)
		if errors.Is(err, ErrNotMember) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !permissions.CanActOn(actor, sub) {
			return ErrHierarchy
		}
		if target, err = s.account(ctx, tx, targetID); err != nil {
			return err
		}
		return s.removeMember(ctx, tx, guildID, targetID)
	})
	if err != nil {
		return err
	}
	s.announceRemoval(ctx, guildID, target)
	return nil
}

// Ban bans targetID, an existing account, from the guild and removes their
// membership if they have one, in one transaction; the actor needs
// BAN_MEMBERS. A member target must rank below the actor (CanActOn); for a
// non-member only the self rule applies (the owner is always a member).
// Banning a banned user changes nothing, its reason included.
func (s *Service) Ban(ctx context.Context, guildID, targetID, actorID snowflake.ID, reason *string) error {
	reason, rerr := normalizeReason(reason)
	if rerr != nil {
		return rerr
	}
	var (
		target  auth.User
		removed bool
		banned  bool
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		removed, banned = false, false
		actor, err := s.moderator(ctx, tx, guildID, actorID, targetID, permissions.BanMembers)
		if err != nil {
			return err
		}
		sub, err := s.subject(ctx, tx, guildID, targetID)
		switch {
		case errors.Is(err, ErrNotMember):
		case err != nil:
			return err
		case !permissions.CanActOn(actor, sub):
			return ErrHierarchy
		default:
			removed = true
		}
		if target, err = s.account(ctx, tx, targetID); err != nil {
			return err
		}
		createdBy := actorID
		n, err := s.q.CreateBan(ctx, tx, store.CreateBanParams{GuildID: guildID, UserID: targetID,
			Reason: reason, CreatedBy: &createdBy, CreatedAt: s.now()})
		if err != nil {
			return fmt.Errorf("guilds: create ban: %w", err)
		}
		banned = n == 1
		if removed {
			return s.removeMember(ctx, tx, guildID, targetID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if removed {
		s.announceRemoval(ctx, guildID, target)
	}
	if banned {
		s.publishBan(ctx, events.GuildBanAdd, guildID, target)
	}
	return nil
}

// Unban lifts the ban of targetID; the actor needs BAN_MEMBERS. A user who
// is not banned is ErrNotFound.
func (s *Service) Unban(ctx context.Context, guildID, targetID, actorID snowflake.ID) error {
	var target auth.User
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		if _, err := s.require(ctx, tx, guildID, actorID, permissions.BanMembers); err != nil {
			return err
		}
		n, err := s.q.DeleteBan(ctx, tx, store.DeleteBanParams{GuildID: guildID, UserID: targetID})
		if err != nil {
			return fmt.Errorf("guilds: delete ban: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		target, err = s.account(ctx, tx, targetID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishBan(ctx, events.GuildBanRemove, guildID, target)
	return nil
}

// Bans returns the bans of the guild, oldest first (at most
// MaxListedBans); the actor needs BAN_MEMBERS.
func (s *Service) Bans(ctx context.Context, guildID, actorID snowflake.ID) ([]Ban, error) {
	if _, err := s.require(ctx, s.pool, guildID, actorID, permissions.BanMembers); err != nil {
		return nil, err
	}
	rows, err := s.q.GuildBans(ctx, s.pool, store.GuildBansParams{GuildID: guildID, Lim: MaxListedBans})
	if err != nil {
		return nil, fmt.Errorf("guilds: list bans: %w", err)
	}
	return s.completeBans(ctx, rows)
}

// BanOf returns the ban of targetID; the actor needs BAN_MEMBERS. A user
// who is not banned is ErrNotFound.
func (s *Service) BanOf(ctx context.Context, guildID, targetID, actorID snowflake.ID) (Ban, error) {
	if _, err := s.require(ctx, s.pool, guildID, actorID, permissions.BanMembers); err != nil {
		return Ban{}, err
	}
	row, err := s.q.BanByID(ctx, s.pool, store.BanByIDParams{GuildID: guildID, UserID: targetID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Ban{}, ErrNotFound
	}
	if err != nil {
		return Ban{}, fmt.Errorf("guilds: load ban: %w", err)
	}
	bans, err := s.completeBans(ctx, []store.GuildBan{row})
	if err != nil {
		return Ban{}, err
	}
	return bans[0], nil
}

// completeBans adds the accounts to stored bans.
func (s *Service) completeBans(ctx context.Context, rows []store.GuildBan) ([]Ban, error) {
	out := make([]Ban, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]snowflake.ID, len(rows))
	for i, r := range rows {
		ids[i] = r.UserID
	}
	users, err := s.users.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[snowflake.ID]auth.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	for i, r := range rows {
		out[i] = Ban{Reason: r.Reason, CreatedAt: r.CreatedAt}
		if u, ok := byID[r.UserID]; ok {
			out[i].User = &u
		}
	}
	return out, nil
}
