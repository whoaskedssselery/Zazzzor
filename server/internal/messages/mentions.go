package messages

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// MaxMentions caps the distinct direct user mentions in a message.
const MaxMentions = 50

// mentions holds the resolved syntax and the users to notify once.
type mentions struct {
	userIDs, roleIDs []int64
	everyone, here   bool
	recipients       []int64
}

// resolveMentions filters the raw syntax using the permission snapshot of the write.
func (s *Service) resolveMentions(ctx context.Context, tx pgx.Tx, channelID, authorID snowflake.ID,
	p permissions.Permission, content string,
) (mentions, error) {
	tokens := parseMentions(content)
	m := mentions{userIDs: []int64{}, roleIDs: []int64{}}
	if len(tokens) == 0 {
		return m, nil
	}
	access, overwrites, err := s.channels.MentionAccessIn(ctx, tx, channelID)
	if err != nil {
		return m, fmt.Errorf("messages: resolve mentions: %w", err)
	}
	users, roles := make(map[snowflake.ID]bool), make(map[snowflake.ID]bool)
	broadcast := p.Has(permissions.MentionEveryone)
	var all, here bool
	for _, token := range tokens {
		switch token.kind {
		case mentionUser:
			if len(m.userIDs) < MaxMentions && !users[token.id] && access.IsMember(token.id) {
				users[token.id] = true
				m.userIDs = append(m.userIDs, int64(token.id))
			}
		case mentionRole:
			if _, exists := access.Role(token.id); exists && token.id != access.GuildID && !roles[token.id] &&
				(broadcast || access.RoleMentionable(token.id)) {
				roles[token.id] = true
				m.roleIDs = append(m.roleIDs, int64(token.id))
			}
		case mentionEveryone:
			all = broadcast
		case mentionHere:
			here = broadcast
		}
	}
	m.everyone, m.here = all || here, here && !all
	m.recipients = s.mentionRecipients(access, overwrites, authorID, users, roles, all, m.here)
	return m, nil
}

// mentionRecipients captures who can see and is addressed, including one presence snapshot for @here.
func (s *Service) mentionRecipients(access guilds.Access, overwrites []permissions.Overwrite, authorID snowflake.ID,
	users, roles map[snowflake.ID]bool, all, here bool,
) []int64 {
	if len(users) == 0 && len(roles) == 0 && !all && !here {
		return nil
	}
	members := access.MemberIDs()
	online := make(map[snowflake.ID]bool)
	if here && s.onlineAmong != nil {
		for _, id := range s.onlineAmong(members) {
			online[id] = true
		}
	}
	var recipients []int64
	for _, id := range members {
		if id == authorID {
			continue
		}
		sub, _ := access.Subject(id)
		target := all || online[id] || users[id]
		if !target {
			for _, role := range sub.Roles {
				if roles[role.ID] {
					target = true
					break
				}
			}
		}
		if target && permissions.InChannel(sub, overwrites).Has(permissions.ViewChannel) {
			recipients = append(recipients, int64(id))
		}
	}
	return recipients
}

// saveMentionRecipients records the audience for later read-state recomputation.
func (s *Service) saveMentionRecipients(ctx context.Context, tx pgx.Tx, id snowflake.ID, m mentions) error {
	if len(m.recipients) == 0 {
		return nil
	}
	if err := s.q.PutMentionRecipients(ctx, tx, store.PutMentionRecipientsParams{MessageID: int64(id), UserIds: m.recipients}); err != nil {
		return fmt.Errorf("messages: store mention recipients: %w", err)
	}
	return nil
}

// storedMentions converts fields from a message without loading its private audience.
func storedMentions(row store.Message) mentions {
	return mentions{userIDs: row.MentionUserIds, roleIDs: row.MentionRoleIds, everyone: row.MentionEveryone, here: row.MentionHere}
}

// mentionIDs converts IDs to the wire type and always returns an array.
func mentionIDs(ids []int64) []snowflake.ID {
	out := make([]snowflake.ID, len(ids))
	for i, id := range ids {
		out[i] = snowflake.ID(id)
	}
	return out
}
