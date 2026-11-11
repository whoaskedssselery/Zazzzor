package channels

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// channelIn loads a channel with its effective overwrites through db (two
// queries); a missing channel is ErrNotFound.
func (s *Service) channelIn(ctx context.Context, db store.DBTX, channelID snowflake.ID) (Channel, error) {
	row, err := s.q.ChannelByID(ctx, db, channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("channels: load channel: %w", err)
	}
	c := toChannel(row)
	from := c.ID
	if c.PermissionSynced && c.ParentID != nil {
		from = *c.ParentID
	}
	ows, err := s.q.OverwritesOf(ctx, db, []int64{int64(from)})
	if err != nil {
		return Channel{}, fmt.Errorf("channels: load overwrites: %w", err)
	}
	c.Overwrites = make([]permissions.Overwrite, len(ows))
	for i, o := range ows {
		c.Overwrites[i] = toOverwrite(o)
	}
	return c, nil
}

// Lookup returns a channel without any permission check (for invite
// previews); a missing channel is ErrNotFound.
func (s *Service) Lookup(ctx context.Context, channelID snowflake.ID) (Channel, error) {
	return s.channelIn(ctx, s.pool, channelID)
}

// viewable loads the channel and userID's permissions in it; a non-member
// or a channel userID may not view is ErrNotFound.
func (s *Service) viewable(ctx context.Context, db store.DBTX, userID, channelID snowflake.ID) (Channel, permissions.Permission, error) {
	c, err := s.channelIn(ctx, db, channelID)
	if err != nil {
		return Channel{}, 0, err
	}
	sub, err := s.guilds.SubjectIn(ctx, db, c.GuildID, userID)
	if errors.Is(err, guilds.ErrNotMember) {
		return Channel{}, 0, ErrNotFound
	}
	if err != nil {
		return Channel{}, 0, err
	}
	p := permissions.InChannel(sub, c.Overwrites)
	if !p.Has(permissions.ViewChannel) {
		return Channel{}, 0, ErrNotFound
	}
	return c, p, nil
}

// Permissions implements Resolver.
func (s *Service) Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	var (
		p       permissions.Permission
		guildID snowflake.ID
	)
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		p, guildID, err = s.PermissionsIn(ctx, tx, userID, channelID)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	return p, guildID, nil
}

// PermissionsIn resolves channel permissions through the caller's transaction.
func (s *Service) PermissionsIn(ctx context.Context, tx pgx.Tx, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	c, p, err := s.viewable(ctx, tx, userID, channelID)
	return p, c.GuildID, err
}

// LockGuild serializes a transaction with membership and permission changes in guildID.
func (s *Service) LockGuild(ctx context.Context, tx pgx.Tx, guildID snowflake.ID) error {
	return s.guilds.LockGuild(ctx, tx, guildID)
}

// Channel returns a channel userID may view, else ErrNotFound.
func (s *Service) Channel(ctx context.Context, userID, channelID snowflake.ID) (Channel, error) {
	var c Channel
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		c, _, err = s.viewable(ctx, tx, userID, channelID)
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	return c, nil
}

// VisibleChannels implements Resolver.
func (s *Service) VisibleChannels(ctx context.Context, userID, guildID snowflake.ID) ([]Channel, error) {
	var out []Channel
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = s.VisibleChannelsIn(ctx, tx, userID, guildID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VisibleChannelsIn lists viewable channels through the caller's transaction.
func (s *Service) VisibleChannelsIn(ctx context.Context, tx pgx.Tx, userID, guildID snowflake.ID) ([]Channel, error) {
	sub, err := s.guilds.SubjectIn(ctx, tx, guildID, userID)
	if err != nil {
		return nil, err
	}
	l, err := loadLayout(ctx, s.q, tx, guildID)
	if err != nil {
		return nil, err
	}
	return l.visibleTo(sub), nil
}

// visibleTo returns the channels sub may view, in display order.
func (l *layout) visibleTo(sub permissions.Subject) []Channel {
	out := []Channel{}
	for _, n := range l.sorted() {
		if l.canView(sub, n) {
			out = append(out, l.view(n))
		}
	}
	return out
}

// Viewers implements Resolver: it loads the channel, its overwrites and the
// whole guild's roles once and computes every member in memory.
func (s *Service) Viewers(ctx context.Context, channelID snowflake.ID) ([]snowflake.ID, error) {
	var out []snowflake.ID
	err := s.read(ctx, func(tx pgx.Tx) error {
		a, overwrites, err := s.MentionAccessIn(ctx, tx, channelID)
		if err != nil {
			return err
		}
		out = viewers(a, overwrites)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MentionAccessIn loads guild roles and member assignments with the channel's
// effective overwrites through the caller's transaction.
func (s *Service) MentionAccessIn(ctx context.Context, tx pgx.Tx, channelID snowflake.ID) (guilds.Access, []permissions.Overwrite, error) {
	c, err := s.channelIn(ctx, tx, channelID)
	if err != nil {
		return guilds.Access{}, nil, err
	}
	a, err := s.guilds.AccessIn(ctx, tx, c.GuildID)
	if errors.Is(err, guilds.ErrNotFound) {
		return guilds.Access{}, nil, ErrNotFound
	}
	if err != nil {
		return guilds.Access{}, nil, err
	}
	return a, c.Overwrites, nil
}

// viewers returns the members of a who may view a channel with the given
// effective overwrites, ascending.
func viewers(a guilds.Access, ows []permissions.Overwrite) []snowflake.ID {
	out := []snowflake.ID{}
	for _, id := range a.MemberIDs() {
		sub, _ := a.Subject(id)
		if permissions.InChannel(sub, ows).Has(permissions.ViewChannel) {
			out = append(out, id)
		}
	}
	return out
}
