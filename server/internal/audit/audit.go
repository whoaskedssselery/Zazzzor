// Package audit keeps a server's audit log: what its moderators and admins
// did (kicks, bans, role, channel, invite and emoji changes, deleted
// messages of others). Other modules record an entry in the transaction of
// the action itself, so the log never shows what did not happen; members
// with VIEW_AUDIT_LOG read it newest first.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Action is what an entry records, numbered as Discord numbers them.
type Action int16

// The actions recorded.
const (
	GuildUpdate      Action = 1
	ChannelCreate    Action = 10
	ChannelUpdate    Action = 11
	ChannelDelete    Action = 12
	OverwriteCreate  Action = 13
	OverwriteUpdate  Action = 14
	OverwriteDelete  Action = 15
	MemberKick       Action = 20
	MemberBanAdd     Action = 22
	MemberBanRemove  Action = 23
	MemberUpdate     Action = 24
	MemberRoleUpdate Action = 25
	MemberMove       Action = 26
	MemberDisconnect Action = 27
	RoleCreate       Action = 30
	RoleUpdate       Action = 31
	RoleDelete       Action = 32
	InviteCreate     Action = 40
	InviteDelete     Action = 42
	EmojiCreate      Action = 60
	EmojiUpdate      Action = 61
	EmojiDelete      Action = 62
	MessageDelete    Action = 72
)

// Known reports whether a is one of the actions above.
func (a Action) Known() bool {
	switch a {
	case GuildUpdate, ChannelCreate, ChannelUpdate, ChannelDelete, OverwriteCreate, OverwriteUpdate, OverwriteDelete,
		MemberKick, MemberBanAdd, MemberBanRemove,
		MemberUpdate, MemberRoleUpdate, MemberMove, MemberDisconnect, RoleCreate, RoleUpdate, RoleDelete,
		InviteCreate, InviteDelete, EmojiCreate, EmojiUpdate, EmojiDelete, MessageDelete:
		return true
	}
	return false
}

// Change is one field an action changed: its value before (none for a
// creation) and after (none for a deletion).
type Change struct {
	Key string `json:"key"`
	Old any    `json:"old_value,omitempty"`
	New any    `json:"new_value,omitempty"`
}

// Entry is one thing done in a guild.
type Entry struct {
	GuildID snowflake.ID
	ActorID snowflake.ID
	Action  Action
	// TargetID is what was acted on; 0 for none (an invite).
	TargetID snowflake.ID
	Changes  []Change
	// Options are extra facts: "channel_id" of a deleted message or moved
	// member, "code" of an invite.
	Options map[string]string
	Reason  *string
}

// Tx is the transaction an entry is written in.
type Tx = store.DBTX

// Recorder is what the other modules depend on. A nil Recorder records
// nothing.
type Recorder interface {
	Record(ctx context.Context, tx Tx, e Entry) error
}

// Record writes e through rec if there is one.
func Record(ctx context.Context, rec Recorder, tx Tx, e Entry) error {
	if rec == nil {
		return nil
	}
	return rec.Record(ctx, tx, e)
}

// Subjects resolves a member's permissions (guilds.Service). For someone
// who is not a member of the guild the error wraps ErrNotFound; any other
// error is a failure.
type Subjects interface {
	Subject(ctx context.Context, guildID, userID snowflake.ID) (permissions.Subject, error)
}

// Users reads the accounts a page names (auth.Service).
type Users interface {
	UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]auth.User, error)
}

// Options configures a Service. Pool, IDs, Subjects and Users are required.
type Options struct {
	Pool     *pgxpool.Pool
	IDs      *snowflake.Generator
	Subjects Subjects
	Users    Users
	Now      func() time.Time // nil: time.Now
}

// Service records and lists entries.
type Service struct {
	pool     *pgxpool.Pool
	q        *store.Queries
	ids      *snowflake.Generator
	subjects Subjects
	users    Users
	now      func() time.Time
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: store.New(), ids: o.IDs, subjects: o.Subjects, users: o.Users, now: o.Now}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

var _ Recorder = (*Service)(nil)

// Record implements Recorder.
func (s *Service) Record(ctx context.Context, tx Tx, e Entry) error {
	changes := e.Changes
	if changes == nil {
		changes = []Change{}
	}
	cj, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("audit: encode changes: %w", err)
	}
	options := e.Options
	if options == nil {
		options = map[string]string{}
	}
	oj, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("audit: encode options: %w", err)
	}
	var actor, target *snowflake.ID
	if e.ActorID != 0 {
		actor = &e.ActorID
	}
	if e.TargetID != 0 {
		target = &e.TargetID
	}
	if err := s.q.InsertEntry(ctx, tx, store.InsertEntryParams{ID: s.ids.Next(), GuildID: e.GuildID, ActorID: actor,
		ActionType: int16(e.Action), TargetID: target, Changes: cj, Options: oj, Reason: e.Reason,
		CreatedAt: s.now()}); err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	return nil
}

// Limits of a page.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Filter selects a page: entries older than Before (0: from the newest),
// by ActorID and of Action when set.
type Filter struct {
	Before  snowflake.ID
	ActorID snowflake.ID
	Action  Action
	Limit   int
}

// PublicEntry is the JSON form of an entry (schema AuditLogEntry).
type PublicEntry struct {
	ID         snowflake.ID      `json:"id"`
	ActionType Action            `json:"action_type"`
	UserID     *snowflake.ID     `json:"user_id"`
	TargetID   *snowflake.ID     `json:"target_id"`
	Changes    json.RawMessage   `json:"changes"`
	Options    map[string]string `json:"options"`
	Reason     *string           `json:"reason"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Page is a page of entries with the users they name (schema AuditLog).
type Page struct {
	Entries []PublicEntry     `json:"audit_log_entries"`
	Users   []auth.PublicUser `json:"users"`
}

// Errors of List.
var (
	ErrNotFound      = errors.New("audit: guild not found")
	ErrMissingPerm   = errors.New("audit: missing VIEW_AUDIT_LOG")
	ErrInvalidFilter = errors.New("audit: invalid filter")
)

// List returns a page of guildID's log for viewerID, who needs
// VIEW_AUDIT_LOG (administrators and the owner have it).
func (s *Service) List(ctx context.Context, guildID, viewerID snowflake.ID, f Filter) (Page, error) {
	sub, err := s.subjects.Subject(ctx, guildID, viewerID)
	if err != nil {
		return Page{}, fmt.Errorf("audit: load viewer: %w", err)
	}
	if !permissions.Base(sub).Has(permissions.ViewAuditLog) {
		return Page{}, ErrMissingPerm
	}
	lim := f.Limit
	if lim == 0 {
		lim = DefaultLimit
	}
	if lim < 1 || lim > MaxLimit || (f.Action != 0 && !f.Action.Known()) {
		return Page{}, ErrInvalidFilter
	}
	rows, err := s.q.ListEntries(ctx, s.pool, store.ListEntriesParams{GuildID: guildID, Before: int64(f.Before),
		ActorID: int64(f.ActorID), ActionType: int16(f.Action), Lim: int32(lim)}) //nolint:gosec // G115: range checked
	if err != nil {
		return Page{}, fmt.Errorf("audit: list: %w", err)
	}
	out := Page{Entries: make([]PublicEntry, len(rows)), Users: []auth.PublicUser{}}
	var ids []snowflake.ID
	seen := map[snowflake.ID]bool{}
	want := func(id *snowflake.ID) {
		if id != nil && !seen[*id] {
			seen[*id] = true
			ids = append(ids, *id)
		}
	}
	for i, r := range rows {
		var options map[string]string
		if err := json.Unmarshal(r.Options, &options); err != nil {
			return Page{}, fmt.Errorf("audit: decode options: %w", err)
		}
		action := Action(r.ActionType)
		out.Entries[i] = PublicEntry{ID: r.ID, ActionType: action, UserID: r.ActorID, TargetID: r.TargetID,
			Changes: r.Changes, Options: options, Reason: r.Reason, CreatedAt: r.CreatedAt}
		want(r.ActorID)
		if targetsUser(action) {
			want(r.TargetID)
		}
	}
	if len(ids) > 0 {
		users, err := s.users.UsersByIDs(ctx, ids)
		if err != nil {
			return Page{}, err
		}
		for _, u := range users {
			out.Users = append(out.Users, u.Public())
		}
	}
	return out, nil
}

// targetsUser reports whether the target of a is a user.
func targetsUser(a Action) bool {
	switch a {
	case MemberKick, MemberBanAdd, MemberBanRemove, MemberUpdate, MemberRoleUpdate, MemberMove, MemberDisconnect,
		MessageDelete:
		return true
	}
	return false
}

// Prune deletes the entries older than keep; it returns how many went.
func (s *Service) Prune(ctx context.Context, keep time.Duration) (int64, error) {
	n, err := s.q.PruneEntries(ctx, s.pool, s.now().Add(-keep))
	if err != nil {
		return 0, fmt.Errorf("audit: prune: %w", err)
	}
	return n, nil
}

// Keep is how long entries stay in the log.
const Keep = 90 * 24 * time.Hour

// RunPrune deletes the entries older than Keep now and then every hour
// until ctx ends; failures are logged to logger.
func (s *Service) RunPrune(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		if _, err := s.Prune(ctx, Keep); err != nil && ctx.Err() == nil {
			logger.Warn("prune audit log", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
