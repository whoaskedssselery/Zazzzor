package guilds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// require loads the actor's subject from the database and checks that its
// guild-level permissions hold need.
func (s *Service) require(ctx context.Context, dbtx store.DBTX, guildID, userID snowflake.ID,
	need permissions.Permission,
) (permissions.Subject, error) {
	sub, err := s.subject(ctx, dbtx, guildID, userID)
	if err != nil {
		return permissions.Subject{}, err
	}
	if !permissions.Base(sub).Has(need) {
		return permissions.Subject{}, ErrMissingPerm
	}
	return sub, nil
}

// lockGuild takes the per-guild lock for role and member-role changes; a
// missing guild is ErrNotMember.
func (s *Service) lockGuild(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error {
	_, err := s.q.LockGuild(ctx, tx, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotMember
	}
	if err != nil {
		return fmt.Errorf("guilds: lock guild: %w", err)
	}
	return nil
}

// checkGuildLimit counts the guilds of userID under its advisory lock.
func (s *Service) checkGuildLimit(ctx context.Context, tx pgx.Tx, userID snowflake.ID) error {
	if err := s.q.LockUserGuilds(ctx, tx, int64(userID)); err != nil {
		return fmt.Errorf("guilds: lock user: %w", err)
	}
	n, err := s.q.CountUserGuilds(ctx, tx, userID)
	if err != nil {
		return fmt.Errorf("guilds: count guilds: %w", err)
	}
	if n >= MaxGuildsPerUser {
		return ErrTooManyGuilds
	}
	return nil
}

// CreateGuild creates a guild owned by owner, with owner as its first member
// and the @everyone role (ID = guild ID, position 0, DefaultEveryone). The
// channels bootstrapper runs in the same transaction. Each call counts
// against the per-user creation limit.
func (s *Service) CreateGuild(ctx context.Context, owner auth.User, name, locale string) (GuildSnapshot, error) {
	if ok, retry := s.createLimiter.Allow(owner.ID.String()); !ok {
		return GuildSnapshot{}, &auth.RateLimitedError{RetryAfter: retry}
	}
	name, nameErr := normalizeName("name", name, 2, 100)
	if nameErr != nil {
		return GuildSnapshot{}, nameErr
	}
	if locale == "" {
		locale = DefaultLocale
	}
	if !slices.Contains(Locales, locale) {
		return GuildSnapshot{}, invalid("locale", "must be one of "+strings.Join(Locales, ", "))
	}
	id := s.ids.Next()
	now := s.now()
	var snap GuildSnapshot
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.checkGuildLimit(ctx, tx, owner.ID); err != nil {
			return err
		}
		g, err := s.q.CreateGuild(ctx, tx, store.CreateGuildParams{ID: id, Name: name, OwnerID: owner.ID, CreatedAt: now})
		if err != nil {
			return fmt.Errorf("guilds: create guild: %w", err)
		}
		everyone, err := s.q.CreateRole(ctx, tx, store.CreateRoleParams{ID: id, GuildID: id, Name: EveryoneName,
			Position: 0, Permissions: mask(permissions.DefaultEveryone), CreatedAt: now})
		if err != nil {
			return fmt.Errorf("guilds: create @everyone: %w", err)
		}
		if _, err = s.q.AddMember(ctx, tx, store.AddMemberParams{GuildID: id, UserID: owner.ID, JoinedAt: now}); err != nil {
			return fmt.Errorf("guilds: add owner: %w", err)
		}
		if err = s.channels.CreateDefaultChannels(ctx, tx, id, locale); err != nil {
			return fmt.Errorf("guilds: create default channels: %w", err)
		}
		snap = GuildSnapshot{Guild: toGuild(g), Roles: []Role{toRole(everyone)},
			Members: []Member{{GuildID: id, User: owner, Roles: []snowflake.ID{}, JoinedAt: now}}}
		return nil
	})
	if err != nil {
		return GuildSnapshot{}, err
	}
	s.publish(ctx, events.Event{Type: events.GuildCreate,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{owner.ID}},
		Payload:  snap.Public()})
	return snap, nil
}

// AddMember adds userID to guildID inside tx (for invites), enforcing the
// per-user guild limit. A banned user is ErrBanned, checked under the
// guild lock that bans take, so a ban racing the join never leaves a
// banned member. It reports false when the user already was a
// member (even at the limit), ErrNotFound when the guild is gone. After
// commit, call AnnounceJoin.
func (s *Service) AddMember(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID) (bool, error) {
	// The per-user lock also serializes this check against a concurrent
	// join of the same user.
	if err := s.q.LockUserGuilds(ctx, tx, int64(userID)); err != nil {
		return false, fmt.Errorf("guilds: lock user: %w", err)
	}
	err := s.lockGuild(ctx, tx, guildID)
	if errors.Is(err, ErrNotMember) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	banned, err := s.q.IsBanned(ctx, tx, store.IsBannedParams{GuildID: guildID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("guilds: check ban: %w", err)
	}
	if banned {
		return false, ErrBanned
	}
	member, err := s.q.IsMember(ctx, tx, store.IsMemberParams{GuildID: guildID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("guilds: check member: %w", err)
	}
	if member {
		return false, nil
	}
	if err = s.checkGuildLimit(ctx, tx, userID); err != nil {
		return false, err
	}
	n, err := s.q.AddMember(ctx, tx, store.AddMemberParams{GuildID: guildID, UserID: userID, JoinedAt: s.now()})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
		return false, ErrNotFound // the guild (or user) is gone
	}
	if err != nil {
		return false, fmt.Errorf("guilds: add member: %w", err)
	}
	if n != 1 {
		return false, nil
	}
	if states, ok := s.overwrites.(readStateLifecycle); ok {
		sub, err := s.subject(ctx, tx, guildID, userID)
		if err != nil {
			return false, fmt.Errorf("guilds: load new member permissions: %w", err)
		}
		if err := states.SeedReadStates(ctx, tx, sub); err != nil {
			return false, fmt.Errorf("guilds: initialize member read states: %w", err)
		}
	}
	return true, nil
}

// AnnounceJoin publishes GUILD_MEMBER_ADD to the guild and GUILD_CREATE
// (the snapshot) to the new member. The member is stored by then, so a
// snapshot that cannot be read still announces the join: GUILD_CREATE to
// the new member and GUILD_SYNC to the rest, both of which the gateway
// fills with the state it reads for each recipient (or it makes them
// reconnect).
func (s *Service) AnnounceJoin(ctx context.Context, guildID, userID snowflake.ID) {
	snap, err := s.Snapshot(ctx, guildID)
	if err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "announce join without a snapshot", "guild_id", guildID.String(),
			"err", err.Error())
		s.publish(ctx, events.Event{Type: events.GuildSync,
			Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID},
			Payload:  GuildSyncPayload{GuildID: guildID}, ExcludeUserID: userID})
		s.publish(ctx, events.Event{Type: events.GuildCreate,
			Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{userID}},
			Payload:  GuildSyncPayload{GuildID: guildID}})
		return
	}
	for _, m := range snap.Members {
		if m.User.ID == userID {
			s.publish(ctx, events.Event{Type: events.GuildMemberAdd,
				Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guildID},
				Payload:  MemberPayload{GuildID: guildID, Member: m.Public()}, ExcludeUserID: userID})
		}
	}
	s.publish(ctx, events.Event{Type: events.GuildCreate,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{userID}},
		Payload:  snap.Public()})
}

// Join adds userID to guildID in its own transaction and announces it.
func (s *Service) Join(ctx context.Context, guildID, userID snowflake.ID) error {
	var added bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		added, err = s.AddMember(ctx, tx, guildID, userID)
		return err
	})
	if err != nil {
		return err
	}
	if added {
		s.AnnounceJoin(ctx, guildID, userID)
	}
	return nil
}

// Guild returns the guild if userID is a member.
func (s *Service) Guild(ctx context.Context, guildID, userID snowflake.ID) (Guild, error) {
	ok, err := s.IsMember(ctx, guildID, userID)
	if err != nil {
		return Guild{}, err
	}
	if !ok {
		return Guild{}, ErrNotMember
	}
	g, err := s.q.GuildByID(ctx, s.pool, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Guild{}, ErrNotMember
	}
	if err != nil {
		return Guild{}, fmt.Errorf("guilds: load guild: %w", err)
	}
	return toGuild(g), nil
}

// GuildsOf returns the guilds of userID (at most MaxGuildsPerUser), by ID.
func (s *Service) GuildsOf(ctx context.Context, userID snowflake.ID) ([]Guild, error) {
	rows, err := s.q.GuildsOfUser(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("guilds: list guilds: %w", err)
	}
	out := make([]Guild, len(rows))
	for i, g := range rows {
		out[i] = toGuild(g)
	}
	return out, nil
}

// Rename changes the name of the guild; the actor needs MANAGE_GUILD.
func (s *Service) Rename(ctx context.Context, guildID, actorID snowflake.ID, name string) (Guild, error) {
	if _, err := s.require(ctx, s.pool, guildID, actorID, permissions.ManageGuild); err != nil {
		return Guild{}, err
	}
	name, err := normalizeName("name", name, 2, 100)
	if err != nil {
		return Guild{}, err
	}
	var g Guild
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		before, loadErr := s.q.GuildByID(ctx, tx, guildID)
		if errors.Is(loadErr, pgx.ErrNoRows) {
			return ErrNotMember
		}
		if loadErr != nil {
			return fmt.Errorf("guilds: load guild: %w", loadErr)
		}
		row, renameErr := s.q.RenameGuild(ctx, tx, store.RenameGuildParams{ID: guildID, Name: name})
		if errors.Is(renameErr, pgx.ErrNoRows) {
			return ErrNotMember
		}
		if renameErr != nil {
			return fmt.Errorf("guilds: rename guild: %w", renameErr)
		}
		g = toGuild(row)
		if before.Name == name {
			return nil
		}
		return s.record(ctx, tx, audit.Entry{GuildID: guildID, ActorID: actorID, Action: audit.GuildUpdate,
			Changes: []audit.Change{{Key: "name", Old: before.Name, New: name}}})
	})
	if err != nil {
		return Guild{}, err
	}
	s.publishGuildUpdate(ctx, g)
	return g, nil
}

func (s *Service) publishGuildUpdate(ctx context.Context, g Guild) {
	s.publish(ctx, events.Event{Type: events.GuildUpdate,
		Audience: events.Audience{Kind: events.AudienceGuild, GuildID: g.ID}, Payload: g.Public()})
}

// CheckManageGuild reports whether actorID may change the guild's settings
// (MANAGE_GUILD); handlers call it before reading an upload.
func (s *Service) CheckManageGuild(ctx context.Context, guildID, actorID snowflake.ID) error {
	_, err := s.require(ctx, s.pool, guildID, actorID, permissions.ManageGuild)
	return err
}

func (s *Service) iconLock(id snowflake.ID) *sync.Mutex {
	return &s.iconLocks[uint64(id)%uint64(len(s.iconLocks))] //nolint:gosec // G115: IDs are positive
}

// SetIcon stores image as the guild's icon (the actor needs MANAGE_GUILD)
// and announces it. Once the file is written the database update runs to
// the end even if the client goes away: on a failed update the new file is
// deleted (unless it was the icon already), after a successful one the
// replaced file is. Failed deletions are only logged.
func (s *Service) SetIcon(ctx context.Context, guildID, actorID snowflake.ID, image []byte) (Guild, error) {
	if err := s.CheckManageGuild(ctx, guildID, actorID); err != nil {
		return Guild{}, err
	}
	mu := s.iconLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	before, err := s.q.GuildByID(ctx, s.pool, guildID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Guild{}, ErrNotMember
	}
	if err != nil {
		return Guild{}, fmt.Errorf("guilds: load guild: %w", err)
	}
	hash, err := s.files.SaveImage(ctx, files.KindIcon, guildID, bytes.NewReader(image))
	if err != nil {
		return Guild{}, err
	}
	row, err := s.q.ReplaceIconHash(context.WithoutCancel(ctx), s.pool, store.ReplaceIconHashParams{ID: guildID, IconHash: &hash})
	if err != nil {
		if before.IconHash == nil || *before.IconHash != hash {
			s.dropIcon(ctx, guildID, &hash)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Guild{}, ErrNotMember
		}
		return Guild{}, fmt.Errorf("guilds: set icon: %w", err)
	}
	g := toGuild(row.Guild)
	if prev := row.PreviousHash; prev == nil || *prev != hash {
		s.dropIcon(ctx, guildID, prev)
		s.publishGuildUpdate(ctx, g)
		// After the icon is in place; a failure here only loses the entry.
		if err := s.record(context.WithoutCancel(ctx), s.pool, audit.Entry{GuildID: guildID, ActorID: actorID,
			Action: audit.GuildUpdate, Changes: []audit.Change{{Key: "icon_hash", Old: prev, New: hash}}}); err != nil {
			httpx.LoggerFrom(ctx).WarnContext(ctx, "audit guild icon", "guild_id", guildID.String(), "err", err.Error())
		}
	}
	return g, nil
}

func (s *Service) dropIcon(ctx context.Context, guildID snowflake.ID, hash *string) {
	if hash == nil {
		return
	}
	if err := s.files.Delete(files.KindIcon, guildID, *hash); err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "delete guild icon",
			"guild_id", guildID.String(), "hash", *hash, "err", err.Error())
	}
}

// DeleteGuild deletes the guild with everything in it; only the owner may.
// Its members get GUILD_DELETE and its icon file is removed after commit.
func (s *Service) DeleteGuild(ctx context.Context, guildID, actorID snowflake.ID) error {
	mu := s.iconLock(guildID)
	mu.Lock()
	defer mu.Unlock()

	var (
		memberIDs []snowflake.ID
		deleted   store.Guild
		emojis    []Emoji
	)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		sub, err := s.subject(ctx, tx, guildID, actorID)
		if err != nil {
			return err
		}
		if sub.OwnerID != actorID {
			return ErrMissingPerm
		}
		if memberIDs, err = s.q.MemberIDs(ctx, tx, guildID); err != nil {
			return fmt.Errorf("guilds: list members: %w", err)
		}
		if emojis, err = s.emojiList(ctx, tx, guildID); err != nil {
			return err
		}
		if deleted, err = s.q.DeleteGuild(ctx, tx, guildID); err != nil {
			return fmt.Errorf("guilds: delete guild: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.dropIcon(ctx, guildID, deleted.IconHash)
	for _, emoji := range emojis {
		s.dropEmoji(ctx, emoji.ID)
	}
	s.publish(ctx, events.Event{Type: events.GuildDelete,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: memberIDs},
		Payload:  GuildDeletePayload{ID: guildID}})
	return nil
}

// Leave removes userID from the guild; the owner cannot leave. The
// remaining members get GUILD_MEMBER_REMOVE, the user GUILD_DELETE.
func (s *Service) Leave(ctx context.Context, guildID snowflake.ID, user auth.User) error {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		sub, err := s.subject(ctx, tx, guildID, user.ID)
		if err != nil {
			return err
		}
		if sub.OwnerID == user.ID {
			return ErrOwnerCannotLeave
		}
		return s.removeMember(ctx, tx, guildID, user.ID)
	})
	if err != nil {
		return err
	}
	s.announceRemoval(ctx, guildID, user)
	return nil
}

// Members returns a page of the guild's members with user IDs above after,
// ascending; the caller must be a member.
func (s *Service) Members(ctx context.Context, guildID, actorID, after snowflake.ID, limit int) ([]Member, error) {
	ok, err := s.IsMember(ctx, guildID, actorID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}
	rows, err := s.q.MembersPage(ctx, s.pool, store.MembersPageParams{GuildID: guildID, After: after, Lim: int32(limit)}) //nolint:gosec // G115: limit is 1..1000
	if err != nil {
		return nil, fmt.Errorf("guilds: list members: %w", err)
	}
	return s.members(ctx, s.pool, guildID, rows)
}
