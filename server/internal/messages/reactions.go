package messages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	maxReactionGroups   = 20
	reactionLockStripes = 64
	defaultReactionPage = 25
	reactionAccess      = permissions.ViewChannel | permissions.ReadMessageHistory
)

// Emoji identifies a reaction. Unicode emoji have a nil ID.
type Emoji struct {
	ID   *snowflake.ID `json:"id"`
	Name string        `json:"name"`
}

// Reaction summarizes an emoji and whether the caller used it.
type Reaction struct {
	Emoji Emoji `json:"emoji"`
	Count int64 `json:"count"`
	Me    bool  `json:"me"`
}

// ReactionPayload describes the user whose reaction changed.
type ReactionPayload struct {
	ChannelID snowflake.ID `json:"channel_id"`
	GuildID   snowflake.ID `json:"guild_id"`
	MessageID snowflake.ID `json:"message_id"`
	UserID    snowflake.ID `json:"user_id"`
	Emoji     Emoji        `json:"emoji"`
}

// Get returns a visible message and its reactions for userID.
func (s *Service) Get(ctx context.Context, channelID, messageID, userID snowflake.ID) (Message, error) {
	_, guildID, err := s.access(ctx, userID, channelID, reactionAccess)
	if err != nil {
		return Message{}, fmt.Errorf("messages: check message access: %w", err)
	}
	row, err := s.load(ctx, channelID, messageID)
	if err != nil {
		return Message{}, fmt.Errorf("messages: get message: %w", err)
	}
	return s.completeOne(ctx, guildID, row, userID)
}

// ReactionUsers lists the users who reacted with emoji, ordered by ID.
func (s *Service) ReactionUsers(ctx context.Context, channelID, messageID, actor snowflake.ID, emoji string, after snowflake.ID, limit int) ([]auth.PublicUser, error) {
	key, emojiID, err := reactionIdentity(emoji)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = defaultReactionPage
	}
	if limit < 1 || limit > MaxLimit {
		return nil, invalid("limit", fmt.Sprintf("must be from 1 to %d", MaxLimit))
	}
	_, guildID, err := s.access(ctx, actor, channelID, reactionAccess)
	if err != nil {
		return nil, fmt.Errorf("messages: check reaction access: %w", err)
	}
	if _, loadErr := s.load(ctx, channelID, messageID); loadErr != nil {
		return nil, fmt.Errorf("messages: find reacted message: %w", loadErr)
	}
	if _, resolveErr := s.resolveReactionEmoji(ctx, s.pool, guildID, emojiID, key, true); resolveErr != nil {
		return nil, resolveErr
	}
	rows, err := s.q.ReactionUserIDs(ctx, s.pool, store.ReactionUserIDsParams{
		MessageID: int64(messageID), Emoji: key, AfterID: int64(after), PageLimit: int32(limit), //nolint:gosec // G115: limit is range checked.
	})
	if err != nil {
		return nil, fmt.Errorf("messages: page reaction users: %w", err)
	}
	ids := make([]snowflake.ID, len(rows))
	for i, id := range rows {
		ids[i] = snowflake.ID(id)
	}
	users, err := s.users.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("messages: load reaction users: %w", err)
	}
	slices.SortFunc(users, func(a, b auth.User) int { return cmp.Compare(a.ID, b.ID) })
	result := make([]auth.PublicUser, len(users))
	for i, user := range users {
		result[i] = user.Public()
	}
	return result, nil
}

// changeReaction serializes reaction writes with permission changes and message
// deletion. A nil target means the caller is changing their own reaction.
func (s *Service) changeReaction(ctx context.Context, channelID, messageID, actor snowflake.ID, emoji string, add bool, target *snowflake.ID) error {
	emojiKey, emojiID, err := reactionIdentity(emoji)
	if err != nil {
		return err
	}
	_, guildID, err := s.access(ctx, actor, channelID, reactionAccess)
	if err != nil {
		return fmt.Errorf("messages: check reaction access: %w", err)
	}
	userID := actor
	if target != nil {
		userID = *target
	}
	payload := ReactionPayload{ChannelID: channelID, GuildID: guildID, MessageID: messageID, UserID: userID}
	// Keep the database commit and its event in order for a message.
	// Mix in the timestamp bits because many Snowflakes have sequence zero.
	key := int64(messageID) ^ messageID.Time().UnixMilli()
	lock := &s.reactionLocks[key&(reactionLockStripes-1)]
	lock.Lock()
	defer lock.Unlock()

	var changed int64
	err = s.withGuildWrite(ctx, guildID, func(tx pgx.Tx) error {
		p, _, accessErr := s.channels.PermissionsIn(ctx, tx, actor, channelID)
		if accessErr != nil {
			return fmt.Errorf("messages: recheck reaction access: %w", accessErr)
		}
		if !p.Has(reactionAccess) {
			return ErrMissingPerm
		}
		_, lookupErr := s.q.MessageInChannel(ctx, tx, store.MessageInChannelParams{ID: messageID, ChannelID: channelID})
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if lookupErr != nil {
			return fmt.Errorf("messages: find reacted message: %w", lookupErr)
		}
		if target != nil {
			if modErr := s.checkReactionModeration(ctx, tx, p, channelID, actor, userID); modErr != nil {
				return fmt.Errorf("messages: authorize reaction removal: %w", modErr)
			}
		}
		resolved, resolveErr := s.resolveReactionEmoji(ctx, tx, guildID, emojiID, emojiKey, !add)
		if resolveErr != nil {
			return resolveErr
		}
		payload.Emoji = resolved
		if add {
			n, addErr := s.addReactionIn(ctx, tx, p, payload, emojiKey)
			changed = n
			return addErr
		}
		n, removeErr := s.q.RemoveMessageReaction(ctx, tx, store.RemoveMessageReactionParams{
			MessageID: int64(messageID), Emoji: emojiKey, UserID: int64(userID),
		})
		if removeErr != nil {
			return fmt.Errorf("messages: delete reaction: %w", removeErr)
		}
		changed = n
		return nil
	})
	if err != nil {
		return fmt.Errorf("messages: change reaction: %w", err)
	}
	if changed != 0 {
		typ := events.MessageReactionRemove
		if add {
			typ = events.MessageReactionAdd
		}
		s.publish(ctx, events.Event{Type: typ, Audience: channelAudience(guildID, channelID), Payload: payload})
	}
	return nil
}

// checkReactionModeration applies the guild's role hierarchy to current members.
func (s *Service) checkReactionModeration(ctx context.Context, tx pgx.Tx, p permissions.Permission, channelID, actor, target snowflake.ID) error {
	if !p.Has(permissions.ManageMessages) {
		return ErrMissingPerm
	}
	if actor == target {
		return nil
	}
	access, _, err := s.channels.MentionAccessIn(ctx, tx, channelID)
	if err != nil {
		return fmt.Errorf("messages: load moderation roles: %w", err)
	}
	member, ok := access.Subject(target)
	if !ok {
		return nil
	}
	moderator, _ := access.Subject(actor)
	if !permissions.CanActOn(moderator, member) {
		return guilds.ErrHierarchy
	}
	return nil
}

// addReactionIn preserves a group's first addition time until its last reaction is removed.
func (s *Service) addReactionIn(ctx context.Context, tx pgx.Tx, p permissions.Permission, r ReactionPayload, key string) (int64, error) {
	groups, err := s.q.ReactionGroupsForWrite(ctx, tx, int64(r.MessageID))
	if err != nil {
		return 0, fmt.Errorf("messages: load reaction groups: %w", err)
	}
	params := store.AddMessageReactionParams{MessageID: int64(r.MessageID), Emoji: key, UserID: int64(r.UserID)}
	if r.Emoji.ID != nil {
		params.EmojiID = dbID(*r.Emoji.ID)
	}
	for _, group := range groups {
		if group.Emoji == key {
			params.FirstAddedAt = &group.FirstAddedAt
			break
		}
	}
	if params.FirstAddedAt == nil {
		if !p.Has(permissions.AddReactions) {
			return 0, ErrMissingPerm
		}
		if len(groups) >= maxReactionGroups {
			return 0, httpx.BadRequest("too_many_reactions", fmt.Sprintf("a message can have at most %d distinct emoji", maxReactionGroups))
		}
	}
	n, err := s.q.AddMessageReaction(ctx, tx, params)
	if err != nil {
		return 0, fmt.Errorf("messages: insert reaction: %w", err)
	}
	return n, nil
}

// reactionSummaries loads the caller-specific reaction groups for one page.
func (s *Service) reactionSummaries(ctx context.Context, ids []int64, caller snowflake.ID) (map[snowflake.ID][]Reaction, error) {
	byMessage := make(map[snowflake.ID][]Reaction)
	rows, err := s.q.ReactionSummaries(ctx, s.pool, store.ReactionSummariesParams{MessageIds: ids, CallerID: int64(caller)})
	if err != nil {
		return nil, fmt.Errorf("messages: load reaction summaries: %w", err)
	}
	for _, row := range rows {
		id := snowflake.ID(row.MessageID)
		emoji := Emoji{Name: row.EmojiName}
		if row.EmojiID.Valid {
			customID := snowflake.ID(row.EmojiID.Int64)
			emoji.ID = &customID
		}
		byMessage[id] = append(byMessage[id], Reaction{Emoji: emoji, Count: row.Count, Me: row.Me})
	}
	return byMessage, nil
}

// badReactionEmoji gives clients the validation requirements for the path segment.
func badReactionEmoji() error {
	return invalid("emoji", "must be one Unicode emoji of at most 32 UTF-8 bytes or a custom emoji in name:id form")
}
