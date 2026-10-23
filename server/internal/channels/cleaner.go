package channels

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Cleaner drops the overwrites of deleted roles and departed members; it
// implements guilds.OverwriteCleaner. Like Bootstrapper it is separate from
// Service because guilds is built first.
type Cleaner struct {
	q *store.Queries
}

var _ guilds.OverwriteCleaner = (*Cleaner)(nil)

// NewCleaner returns a Cleaner.
func NewCleaner() *Cleaner { return &Cleaner{q: store.New()} }

func (c *Cleaner) drop(ctx context.Context, tx pgx.Tx, guildID, targetID snowflake.ID) error {
	if err := c.q.DeleteTargetOverwrites(ctx, tx, store.DeleteTargetOverwritesParams{GuildID: guildID, TargetID: targetID}); err != nil {
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
