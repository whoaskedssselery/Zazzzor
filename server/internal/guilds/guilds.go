// Package guilds owns servers (guilds), their members and roles. Other
// modules read them through Directory; channels plug into guild creation
// through ChannelsBootstrapper.
package guilds

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// Limits.
const (
	// MaxGuildsPerUser is how many guilds one user may be a member of.
	MaxGuildsPerUser = 100
	// MaxRoles is how many roles (with @everyone) a guild may have.
	MaxRoles = 250
	// EveryoneName is the name of the @everyone role.
	EveryoneName = "@everyone"
)

// Guild creations per user: 5 per hour.
var (
	createRate  = rate.Every(time.Hour / 5)
	createBurst = 5
	createTTL   = 2 * time.Hour
)

// Guild, role and member writes per user, shared: 30 per minute.
var (
	writeRate  = rate.Every(time.Minute / 30)
	writeBurst = 30
	writeTTL   = 2 * time.Minute
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	// ErrNotMember: the guild does not exist or the user is not a member;
	// both look the same to clients (404).
	ErrNotMember = errors.New("guilds: not a member")
	// ErrNotFound: the role or member addressed does not exist in the guild.
	ErrNotFound         = errors.New("guilds: not found")
	ErrMissingPerm      = errors.New("guilds: missing permission")
	ErrTooManyGuilds    = errors.New("guilds: too many guilds")
	ErrTooManyRoles     = errors.New("guilds: too many roles")
	ErrOwnerCannotLeave = errors.New("guilds: owner cannot leave")
	ErrEveryone         = errors.New("guilds: the @everyone role cannot be deleted")
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
	return "guilds: invalid " + strings.Join(names, ", ")
}

func invalid(field, msg string) error {
	return &ValidationError{Fields: []httpx.FieldError{{Field: field, Message: msg}}}
}

// ToProblem maps the errors of this package (and of auth and files) to
// their HTTP problem; any other error is returned unchanged.
func ToProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		return httpx.BadRequest("validation_failed", "some fields are invalid", verr.Fields...)
	}
	if rl, ok := errors.AsType[*auth.RateLimitedError](err); ok {
		return httpx.TooManyRequests(rl.RetryAfter)
	}
	switch {
	case errors.Is(err, ErrNotMember), errors.Is(err, ErrNotFound):
		return httpx.NotFound()
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	case errors.Is(err, ErrTooManyGuilds):
		return httpx.BadRequest("too_many_guilds", fmt.Sprintf("a user can be in at most %d servers", MaxGuildsPerUser))
	case errors.Is(err, ErrTooManyRoles):
		return httpx.BadRequest("too_many_roles", fmt.Sprintf("a server can have at most %d roles", MaxRoles))
	case errors.Is(err, ErrOwnerCannotLeave):
		return httpx.BadRequest("owner_cannot_leave", "the owner cannot leave their server; delete it instead")
	case errors.Is(err, ErrEveryone):
		return httpx.BadRequest("cannot_delete_everyone", "the @everyone role cannot be deleted")
	case errors.Is(err, ErrCannotTargetSelf):
		return httpx.BadRequest("cannot_target_self", "use DELETE /users/@me/guilds/{guild_id} to leave a server")
	case errors.Is(err, ErrHierarchy):
		return httpx.Forbidden("role_hierarchy")
	case errors.Is(err, ErrBanned):
		return httpx.Forbidden("banned")
	}
	return files.ToProblem(err)
}

// Guild is a server.
type Guild struct {
	ID        snowflake.ID
	Name      string
	IconHash  *string
	OwnerID   snowflake.ID
	CreatedAt time.Time
}

// PublicGuild is the JSON form of a Guild (schema Guild).
type PublicGuild struct {
	ID        snowflake.ID `json:"id"`
	Name      string       `json:"name"`
	IconURL   *string      `json:"icon_url"`
	OwnerID   snowflake.ID `json:"owner_id"`
	CreatedAt string       `json:"created_at"`
}

// Public returns the JSON form of g.
func (g Guild) Public() PublicGuild {
	p := PublicGuild{ID: g.ID, Name: g.Name, OwnerID: g.OwnerID, CreatedAt: httpx.FormatTime(g.CreatedAt)}
	if g.IconHash != nil {
		url := "/api/v1/files/icons/" + g.ID.String() + "/" + *g.IconHash + ".png"
		p.IconURL = &url
	}
	return p
}

// Role is a role of a guild. @everyone has the guild's ID and position 0.
type Role struct {
	ID          snowflake.ID
	GuildID     snowflake.ID
	Name        string
	Color       int
	Position    int
	Permissions permissions.Permission
	Hoist       bool
	Mentionable bool
}

// PublicRole is the JSON form of a Role (schema Role).
type PublicRole struct {
	ID          snowflake.ID           `json:"id"`
	Name        string                 `json:"name"`
	Color       int                    `json:"color"`
	Position    int                    `json:"position"`
	Permissions permissions.Permission `json:"permissions"`
	Hoist       bool                   `json:"hoist"`
	Mentionable bool                   `json:"mentionable"`
}

// Public returns the JSON form of r.
func (r Role) Public() PublicRole {
	return PublicRole{ID: r.ID, Name: r.Name, Color: r.Color, Position: r.Position,
		Permissions: r.Permissions, Hoist: r.Hoist, Mentionable: r.Mentionable}
}

func (r Role) perm() permissions.Role {
	return permissions.Role{ID: r.ID, Position: r.Position, Permissions: r.Permissions}
}

// Member is a member of a guild.
type Member struct {
	GuildID snowflake.ID
	User    auth.User
	Nick    *string
	// Roles are the IDs of the member's roles, without @everyone, ascending.
	Roles    []snowflake.ID
	JoinedAt time.Time
}

// PublicMember is the JSON form of a Member (schema Member).
type PublicMember struct {
	User     auth.PublicUser `json:"user"`
	Nick     *string         `json:"nick"`
	Roles    []snowflake.ID  `json:"roles"`
	JoinedAt string          `json:"joined_at"`
}

// Public returns the JSON form of m.
func (m Member) Public() PublicMember {
	roles := m.Roles
	if roles == nil {
		roles = []snowflake.ID{}
	}
	return PublicMember{User: m.User.Public(), Nick: m.Nick, Roles: roles, JoinedAt: httpx.FormatTime(m.JoinedAt)}
}

// GuildSnapshot is everything about a guild a member's client needs on
// READY and GUILD_SYNC (channels are added by the gateway).
type GuildSnapshot struct {
	Guild   Guild
	Roles   []Role   // by position
	Members []Member // by user ID
	Emojis  []Emoji  // active, by ID
}

// PublicSnapshot is the JSON form of a GuildSnapshot.
type PublicSnapshot struct {
	Guild   PublicGuild    `json:"guild"`
	Roles   []PublicRole   `json:"roles"`
	Members []PublicMember `json:"members"`
}

// Public returns the JSON form of s.
func (s GuildSnapshot) Public() PublicSnapshot {
	p := PublicSnapshot{Guild: s.Guild.Public(), Roles: make([]PublicRole, len(s.Roles)),
		Members: make([]PublicMember, len(s.Members))}
	for i, r := range s.Roles {
		p.Roles[i] = r.Public()
	}
	for i, m := range s.Members {
		p.Members[i] = m.Public()
	}
	return p
}

// Event payloads.
type (
	// GuildDeletePayload is the payload of GUILD_DELETE: the guild is gone,
	// or the recipient left it.
	GuildDeletePayload struct {
		ID snowflake.ID `json:"id"`
	}
	// GuildSyncPayload is the payload of GUILD_SYNC; the gateway sends each
	// member their own snapshot of the guild.
	GuildSyncPayload struct {
		GuildID snowflake.ID `json:"guild_id"`
	}
	// MemberPayload is the payload of GUILD_MEMBER_ADD and _UPDATE.
	MemberPayload struct {
		GuildID snowflake.ID `json:"guild_id"`
		Member  PublicMember `json:"member"`
	}
	// MemberRemovePayload is the payload of GUILD_MEMBER_REMOVE.
	MemberRemovePayload struct {
		GuildID snowflake.ID    `json:"guild_id"`
		User    auth.PublicUser `json:"user"`
	}
	// RolePayload is the payload of GUILD_ROLE_CREATE and _UPDATE.
	RolePayload struct {
		GuildID snowflake.ID `json:"guild_id"`
		Role    PublicRole   `json:"role"`
	}
	// RoleDeletePayload is the payload of GUILD_ROLE_DELETE.
	RoleDeletePayload struct {
		GuildID snowflake.ID `json:"guild_id"`
		RoleID  snowflake.ID `json:"role_id"`
	}
)

// Directory is what other modules (channels, messages, gateway) read about
// guilds.
type Directory interface {
	// Subject returns userID as a permission subject in guildID, or
	// ErrNotMember.
	Subject(ctx context.Context, guildID, userID snowflake.ID) (permissions.Subject, error)
	IsMember(ctx context.Context, guildID, userID snowflake.ID) (bool, error)
	// MemberIDs returns the members of guildID, ascending.
	MemberIDs(ctx context.Context, guildID snowflake.ID) ([]snowflake.ID, error)
	// GuildIDsOf returns the guilds userID is a member of, ascending.
	GuildIDsOf(ctx context.Context, userID snowflake.ID) ([]snowflake.ID, error)
	// Snapshot returns the guild with its roles and members, or ErrNotFound.
	Snapshot(ctx context.Context, guildID snowflake.ID) (GuildSnapshot, error)
	// PeerIDs returns everyone sharing a guild or friendship with userID
	// (userID too, if it is in any guild), ascending.
	PeerIDs(ctx context.Context, userID snowflake.ID) ([]snowflake.ID, error)
	// SharedPeers returns, for each of userIDs, which of peerIDs
	// share at least one guild with it (itself included when listed), in
	// one query.
	SharedPeers(ctx context.Context, userIDs, peerIDs []snowflake.ID) (map[snowflake.ID][]snowflake.ID, error)
	// MemberIDsWith returns the members of guildID whose guild-level
	// permissions hold need, ascending; none for a missing guild.
	MemberIDsWith(ctx context.Context, guildID snowflake.ID, need permissions.Permission) ([]snowflake.ID, error)
}

var _ Directory = (*Service)(nil)

// ChannelsBootstrapper creates the channels of a new guild inside the
// transaction that creates the guild; an error rolls the creation back.
type ChannelsBootstrapper interface {
	CreateDefaultChannels(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error
}

type noChannels struct{}

func (noChannels) CreateDefaultChannels(context.Context, pgx.Tx, snowflake.ID) error { return nil }

// OverwriteCleaner drops the channel overwrites that target a role or member
// leaving the guild, inside the transaction (holding the guild lock) that
// removes it; an error rolls the removal back. Without it a member who
// rejoins would get back their old per-channel rights.
type OverwriteCleaner interface {
	RoleDeleted(ctx context.Context, tx pgx.Tx, guildID, roleID snowflake.ID) error
	MemberRemoved(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID) error
}

// readStateLifecycle runs alongside membership and channel visibility changes.
type readStateLifecycle interface {
	SeedReadStates(ctx context.Context, tx pgx.Tx, sub permissions.Subject) error
	PruneReadStates(ctx context.Context, tx pgx.Tx, a Access) error
}

type noCleaner struct{}

func (noCleaner) RoleDeleted(context.Context, pgx.Tx, snowflake.ID, snowflake.ID) error   { return nil }
func (noCleaner) MemberRemoved(context.Context, pgx.Tx, snowflake.ID, snowflake.ID) error { return nil }

// Hooks connect other modules to guild life-cycle steps.
type Hooks struct {
	// Channels runs on guild creation; nil does nothing.
	Channels ChannelsBootstrapper
	// Overwrites runs on role deletion and member removal. It also maintains
	// read states when the cleaner supports the read-state lifecycle.
	Overwrites OverwriteCleaner
}

// Users is what guilds needs from the auth module.
type Users interface {
	UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]auth.User, error)
	// UsersByIDsIn reads the accounts through dbtx: the transaction a guild
	// read holds, so it never waits for a second pool connection.
	UsersByIDsIn(ctx context.Context, dbtx auth.DB, ids []snowflake.ID) ([]auth.User, error)
}

var _ Users = (auth.Accounts)(nil)

// Options configures a Service. Pool, IDs, Users and Files are required.
type Options struct {
	Pool      *pgxpool.Pool
	IDs       *snowflake.Generator
	Users     Users
	Files     files.Store
	Publisher events.Publisher // nil: events are dropped
	Hooks     Hooks
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// CreateLimiter limits guild creations per user; nil takes the default
	// (5 per hour).
	CreateLimiter *ratelimit.Limiter
	// WriteLimiter limits guild, role and member writes per user; nil takes
	// the default (30 per minute).
	WriteLimiter *ratelimit.Limiter
}

// Service implements guilds, members and roles.
type Service struct {
	pool          *pgxpool.Pool
	q             *store.Queries
	ids           *snowflake.Generator
	users         Users
	files         files.Store
	emojiFiles    *files.Disk
	pub           events.Publisher
	channels      ChannelsBootstrapper
	overwrites    OverwriteCleaner
	now           func() time.Time
	createLimiter *ratelimit.Limiter
	writeLimiter  *ratelimit.Limiter
	// iconLocks serialize the icon changes of one guild (striped by ID), so
	// that a replaced file is never deleted after a concurrent upload of the
	// same image made it current again.
	iconLocks [64]sync.Mutex
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: store.New(), ids: o.IDs, users: o.Users, files: o.Files, pub: o.Publisher,
		channels: o.Hooks.Channels, overwrites: o.Hooks.Overwrites, now: o.Now, createLimiter: o.CreateLimiter, writeLimiter: o.WriteLimiter}
	if s.channels == nil {
		s.channels = noChannels{}
	}
	if s.overwrites == nil {
		s.overwrites = noCleaner{}
	}
	s.emojiFiles, _ = o.Files.(*files.Disk)
	if s.now == nil {
		s.now = time.Now
	}
	if s.createLimiter == nil {
		s.createLimiter = ratelimit.New(createRate, createBurst, createTTL)
	}
	if s.writeLimiter == nil {
		s.writeLimiter = ratelimit.New(writeRate, writeBurst, writeTTL)
	}
	return s
}

func (s *Service) publish(ctx context.Context, e events.Event) {
	if s.pub != nil {
		s.pub.Publish(ctx, e)
	}
}

func (s *Service) publishSync(ctx context.Context, guildID snowflake.ID) {
	s.publish(ctx, events.Event{Type: events.GuildSync,
		Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID},
		Payload:  GuildSyncPayload{GuildID: guildID}})
}

func toGuild(g store.Guild) Guild {
	return Guild{ID: g.ID, Name: g.Name, IconHash: g.IconHash, OwnerID: g.OwnerID, CreatedAt: g.CreatedAt}
}

func toRole(r store.Role) Role {
	return Role{ID: r.ID, GuildID: r.GuildID, Name: r.Name, Color: int(r.Color), Position: int(r.Position),
		Permissions: permissions.Permission(r.Permissions), //nolint:gosec // G115: stored masks are within All
		Hoist:       r.Hoist, Mentionable: r.Mentionable}
}

func mask(p permissions.Permission) int64 {
	return int64(p) //nolint:gosec // G115: masks are checked against permissions.All, which fits
}

func ids64(ids []snowflake.ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// normalizeName applies textx.Name to s, reporting a problem on field.
func normalizeName(field, s string, minLen, maxLen int) (string, error) {
	name, problem := textx.Name(s, minLen, maxLen)
	if problem != "" {
		return "", invalid(field, problem)
	}
	return name, nil
}

// --- Directory ---

// subject loads userID's permission subject in guildID through db.
func (s *Service) subject(ctx context.Context, db store.DBTX, guildID, userID snowflake.ID) (permissions.Subject, error) {
	row, err := s.q.MemberSubject(ctx, db, store.MemberSubjectParams{GuildID: guildID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return permissions.Subject{}, ErrNotMember
	}
	if err != nil {
		return permissions.Subject{}, fmt.Errorf("guilds: load member: %w", err)
	}
	roles, err := s.q.RolesOfMember(ctx, db, store.RolesOfMemberParams{GuildID: guildID, UserID: userID})
	if err != nil {
		return permissions.Subject{}, fmt.Errorf("guilds: load member roles: %w", err)
	}
	sub := permissions.Subject{
		UserID: userID, OwnerID: row.OwnerID, GuildID: guildID,
		Everyone: permissions.Role{ID: guildID, Position: int(row.EveryonePosition),
			Permissions: permissions.Permission(row.EveryonePermissions)}, //nolint:gosec // G115: see toRole
		Roles: make([]permissions.Role, len(roles)),
	}
	for i, r := range roles {
		sub.Roles[i] = toRole(r).perm()
	}
	return sub, nil
}

// Subject implements Directory.
func (s *Service) Subject(ctx context.Context, guildID, userID snowflake.ID) (permissions.Subject, error) {
	return s.subject(ctx, s.pool, guildID, userID)
}

// IsMember implements Directory.
func (s *Service) IsMember(ctx context.Context, guildID, userID snowflake.ID) (bool, error) {
	ok, err := s.q.IsMember(ctx, s.pool, store.IsMemberParams{GuildID: guildID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("guilds: check member: %w", err)
	}
	return ok, nil
}

// MemberIDs implements Directory.
func (s *Service) MemberIDs(ctx context.Context, guildID snowflake.ID) ([]snowflake.ID, error) {
	ids, err := s.q.MemberIDs(ctx, s.pool, guildID)
	if err != nil {
		return nil, fmt.Errorf("guilds: list members: %w", err)
	}
	return ids, nil
}

// GuildIDsOf implements Directory.
func (s *Service) GuildIDsOf(ctx context.Context, userID snowflake.ID) ([]snowflake.ID, error) {
	ids, err := s.q.GuildIDsOfUser(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("guilds: list guilds: %w", err)
	}
	return ids, nil
}

// PeerIDs implements Directory.
func (s *Service) PeerIDs(ctx context.Context, userID snowflake.ID) ([]snowflake.ID, error) {
	ids, err := s.q.PeerIDs(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("guilds: list peers: %w", err)
	}
	return ids, nil
}

// SharedPeers implements Directory.
func (s *Service) SharedPeers(ctx context.Context, userIDs, peerIDs []snowflake.ID) (map[snowflake.ID][]snowflake.ID, error) {
	out := make(map[snowflake.ID][]snowflake.ID, len(userIDs))
	if len(userIDs) == 0 || len(peerIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.SharedPeers(ctx, s.pool, store.SharedPeersParams{UserIds: ids64(userIDs), PeerIds: ids64(peerIDs)})
	if err != nil {
		return nil, fmt.Errorf("guilds: list shared peers: %w", err)
	}
	for _, r := range rows {
		out[r.UserID] = append(out[r.UserID], r.PeerID)
	}
	return out, nil
}

// MemberIDsWith implements Directory. It reads the guild's access in one
// repeatable-read transaction.
func (s *Service) MemberIDsWith(ctx context.Context, guildID snowflake.ID, need permissions.Permission) ([]snowflake.ID, error) {
	var out []snowflake.ID
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			out = nil
			a, err := s.AccessIn(ctx, tx, guildID)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			for _, id := range a.memberIDs {
				if sub, ok := a.Subject(id); ok && permissions.Base(sub).Has(need) {
					out = append(out, id)
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Snapshot implements Directory. It reads in one repeatable-read
// transaction, so guild, roles and members are consistent.
func (s *Service) Snapshot(ctx context.Context, guildID snowflake.ID) (GuildSnapshot, error) {
	var snap GuildSnapshot
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			g, err := s.q.GuildByID(ctx, tx, guildID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("guilds: load guild: %w", err)
			}
			snap.Guild = toGuild(g)
			if snap.Roles, err = s.roles(ctx, tx, guildID); err != nil {
				return err
			}
			if snap.Emojis, err = s.emojiList(ctx, tx, guildID); err != nil {
				return err
			}
			rows, err := s.q.AllMembers(ctx, tx, guildID)
			if err != nil {
				return fmt.Errorf("guilds: list members: %w", err)
			}
			snap.Members, err = s.members(ctx, tx, guildID, rows)
			return err
		})
	if err != nil {
		return GuildSnapshot{}, err
	}
	return snap, nil
}

func (s *Service) roles(ctx context.Context, db store.DBTX, guildID snowflake.ID) ([]Role, error) {
	rows, err := s.q.GuildRoles(ctx, db, guildID)
	if err != nil {
		return nil, fmt.Errorf("guilds: list roles: %w", err)
	}
	out := make([]Role, len(rows))
	for i, r := range rows {
		out[i] = toRole(r)
	}
	return out, nil
}

// members completes member rows with their roles and accounts.
func (s *Service) members(ctx context.Context, db store.DBTX, guildID snowflake.ID, rows []store.Member) ([]Member, error) {
	out := make([]Member, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]snowflake.ID, len(rows))
	for i, m := range rows {
		ids[i] = m.UserID
	}
	roleRows, err := s.q.MemberRoleIDs(ctx, db, store.MemberRoleIDsParams{GuildID: guildID, UserIds: ids64(ids)})
	if err != nil {
		return nil, fmt.Errorf("guilds: list member roles: %w", err)
	}
	roles := make(map[snowflake.ID][]snowflake.ID, len(rows))
	for _, r := range roleRows {
		roles[r.UserID] = append(roles[r.UserID], r.RoleID)
	}
	users, err := s.users.UsersByIDsIn(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[snowflake.ID]auth.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	for _, m := range rows {
		u, ok := byID[m.UserID]
		if !ok {
			continue // the account is gone; its membership cascades away
		}
		out = append(out, Member{GuildID: guildID, User: u, Nick: m.Nick, Roles: roles[m.UserID], JoinedAt: m.JoinedAt})
	}
	return out, nil
}

func (s *Service) member(ctx context.Context, db store.DBTX, guildID, userID snowflake.ID) (Member, error) {
	row, err := s.q.MemberByID(ctx, db, store.MemberByIDParams{GuildID: guildID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("guilds: load member: %w", err)
	}
	ms, err := s.members(ctx, db, guildID, []store.Member{row})
	if err != nil {
		return Member{}, err
	}
	if len(ms) == 0 {
		return Member{}, ErrNotFound
	}
	return ms[0], nil
}
