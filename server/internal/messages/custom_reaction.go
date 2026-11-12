package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// reactionIdentity uses the exact Unicode bytes or a stable key based on the
// custom emoji ID. The name in the URL may predate a rename.
func reactionIdentity(raw string) (string, snowflake.ID, error) {
	name, rawID, custom := strings.Cut(raw, ":")
	if !custom {
		if !validReactionEmoji(raw) {
			return "", 0, badReactionEmoji()
		}
		return raw, 0, nil
	}
	id, err := snowflake.Parse(rawID)
	if err != nil || id <= 0 || id.String() != rawID || !guilds.ValidEmojiName(name) {
		return "", 0, badReactionEmoji()
	}
	return "c:" + id.String(), id, nil
}

// resolveReactionEmoji verifies guild scope and supplies the name clients
// display. Archived emoji remain readable and removable.
func (s *Service) resolveReactionEmoji(ctx context.Context, q store.DBTX, guildID, id snowflake.ID, key string, includeDeleted bool) (Emoji, error) {
	if id == 0 {
		return Emoji{Name: key}, nil
	}
	if guildID == 0 {
		return Emoji{}, invalid("emoji", "custom reactions are only available in a server")
	}
	name, err := s.q.CustomReactionEmoji(ctx, q, store.CustomReactionEmojiParams{
		ID: int64(id), GuildID: int64(guildID), IncludeDeleted: includeDeleted,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Emoji{}, invalid("emoji", "custom emoji must exist in this server")
	}
	if err != nil {
		return Emoji{}, fmt.Errorf("messages: load custom emoji: %w", err)
	}
	return Emoji{ID: &id, Name: name}, nil
}
