package channels

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Cleaner manages read states across membership and role changes and drops
// the overwrites of deleted roles and departed members. Like Bootstrapper,
// it is separate from Service because guilds is built first.
type Cleaner struct {
	q *store.Queries
}

var _ guilds.OverwriteCleaner = (*Cleaner)(nil)

// NewCleaner returns a Cleaner.
func NewCleaner() *Cleaner { return &Cleaner{q: store.New()} }

// SeedReadStates starts a new member at the head of their visible text channels.
func (c *Cleaner) SeedReadStates(ctx context.Context, tx pgx.Tx, sub permissions.Subject) error {
	l, err := loadLayout(ctx, c.q, tx, sub.GuildID)
	if err != nil {
		return fmt.Errorf("channels: load layout for initial read states: %w", err)
	}
	var ids []int64
	for id, n := range l.nodes {
		if n.ch.Type == TypeText && l.canView(sub, n) {
			ids = append(ids, int64(id))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := c.q.SeedReadStates(ctx, tx, store.SeedReadStatesParams{UserID: int64(sub.UserID), ChannelIds: ids}); err != nil {
		return fmt.Errorf("channels: seed read states: %w", err)
	}
	return nil
}

// PruneReadStates drops states for channels their member can no longer see.
func (c *Cleaner) PruneReadStates(ctx context.Context, tx pgx.Tx, a guilds.Access) error {
	l, err := loadLayout(ctx, c.q, tx, a.GuildID)
	if err != nil {
		return fmt.Errorf("channels: load layout for read-state cleanup: %w", err)
	}
	return l.pruneReadStates(ctx, c.q, tx, a)
}

// pruneReadStates checks existing state keys using the updated effective overwrites.
func (l *layout) pruneReadStates(ctx context.Context, q *store.Queries, tx pgx.Tx, a guilds.Access) error {
	rows, err := q.GuildReadStateKeys(ctx, tx, int64(l.guildID))
	if err != nil {
		return fmt.Errorf("channels: list read state keys: %w", err)
	}
	p := store.DropReadStatesParams{GuildID: int64(l.guildID)}
	subjects := make(map[snowflake.ID]permissions.Subject)
	for _, row := range rows {
		userID := snowflake.ID(row.UserID)
		sub, ok := subjects[userID]
		if !ok {
			sub, _ = a.Subject(userID)
			subjects[userID] = sub
		}
		if n := l.nodes[snowflake.ID(row.ChannelID)]; n != nil && l.canView(sub, n) {
			continue
		}
		p.UserIds = append(p.UserIds, row.UserID)
		p.ChannelIds = append(p.ChannelIds, row.ChannelID)
	}
	if len(p.UserIds) > 0 {
		if err := q.DropReadStates(ctx, tx, p); err != nil {
			return fmt.Errorf("channels: drop hidden read states: %w", err)
		}
	}
	return nil
}

func (c *Cleaner) drop(ctx context.Context, tx pgx.Tx, guildID, targetID snowflake.ID) error {
	if err := c.q.DeleteTargetOverwrites(ctx, tx, store.DeleteTargetOverwritesParams{GuildID: &guildID, TargetID: targetID}); err != nil {
		return fmt.Errorf("channels: drop overwrites: %w", err)
	}
	return nil
}

// RoleDeleted implements guilds.OverwriteCleaner.
func (c *Cleaner) RoleDeleted(ctx context.Context, tx pgx.Tx, guildID, roleID snowflake.ID) error {
	return c.drop(ctx, tx, guildID, roleID)
}

// MemberRemoved implements guilds.OverwriteCleaner.
func (c *Cleaner) MemberRemoved(ctx context.Context, tx pgx.Tx, guildID, userID snowflake.ID) error {
	return c.drop(ctx, tx, guildID, userID)
}
