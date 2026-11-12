package messages

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// withChannelWrite uses the membership lock for the channel's scope.
func (s *Service) withChannelWrite(ctx context.Context, guildID, channelID snowflake.ID, change func(pgx.Tx) error) error {
	if guildID != 0 {
		return s.withGuildWrite(ctx, guildID, change)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.channels.LockPrivateIn(ctx, tx, channelID); err != nil {
			return err
		}
		return change(tx)
	})
}

// nullableGuildID encodes private channels as null at the API boundary.
func nullableGuildID(id snowflake.ID) *snowflake.ID {
	if id == 0 {
		return nil
	}
	return &id
}

// dbGuildID bypasses guild membership FKs only for recipient-scoped read states.
func dbGuildID(id snowflake.ID) pgtype.Int8 {
	return pgtype.Int8{Int64: int64(id), Valid: id != 0}
}

// privateMentions resolves direct mentions and captures every other
// recipient for the DM's unread badge. Role and broadcast tokens are inert.
func (s *Service) privateMentions(ctx context.Context, tx pgx.Tx, id, author snowflake.ID, tokens []mentionToken) (mentions, error) {
	m := mentions{userIDs: []int64{}, roleIDs: []int64{}}
	ids, err := s.channels.PrivateRecipientsIn(ctx, tx, id)
	if err != nil {
		return m, fmt.Errorf("messages: resolve private recipients: %w", err)
	}
	members := make(map[snowflake.ID]bool, len(ids))
	for _, user := range ids {
		members[user] = true
		if user != author {
			m.recipients = append(m.recipients, int64(user))
		}
	}
	for _, token := range tokens {
		if token.kind == mentionUser && members[token.id] && len(m.userIDs) < MaxMentions {
			m.userIDs = append(m.userIDs, int64(token.id))
			delete(members, token.id)
		}
	}
	return m, nil
}
