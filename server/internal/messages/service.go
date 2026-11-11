package messages

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// access returns userID's permissions in the channel and its guild after
// checking they hold need; a channel userID may not view is
// channels.ErrNotFound.
func (s *Service) access(ctx context.Context, userID, channelID snowflake.ID, need permissions.Permission) (permissions.Permission, snowflake.ID, error) {
	p, guildID, err := s.channels.Permissions(ctx, userID, channelID)
	if err != nil {
		return 0, 0, err
	}
	if !p.Has(need) {
		return 0, 0, ErrMissingPerm
	}
	return p, guildID, nil
}

// requireText checks the channel is a text channel.
func (s *Service) requireText(ctx context.Context, channelID snowflake.ID) error {
	c, err := s.channels.Lookup(ctx, channelID)
	if err != nil {
		return err
	}
	if c.Type != channels.TypeText {
		return ErrNotTextChannel
	}
	return nil
}

// Page selects a page of history: Before (or neither) gives the newest
// messages older than it, newest first; After the oldest newer than it,
// oldest first. Zero means unset; setting both is the caller's error.
type Page struct {
	Before snowflake.ID
	After  snowflake.ID
	// Limit 0 takes DefaultLimit.
	Limit int
}

// List returns a page of the channel's history; userID needs
// READ_MESSAGE_HISTORY there.
func (s *Service) List(ctx context.Context, channelID, userID snowflake.ID, pg Page) ([]Message, error) {
	_, guildID, err := s.access(ctx, userID, channelID, permissions.ReadMessageHistory)
	if err != nil {
		return nil, err
	}
	lim := pg.Limit
	if lim == 0 {
		lim = DefaultLimit
	}
	if lim < 1 || lim > MaxLimit {
		return nil, invalid("limit", fmt.Sprintf("must be from 1 to %d", MaxLimit))
	}
	if pg.Before != 0 && pg.After != 0 {
		return nil, invalid("before", "cannot be combined with after")
	}
	var rows []store.Message
	if pg.After != 0 {
		rows, err = s.q.MessagesAfter(ctx, s.pool, store.MessagesAfterParams{ChannelID: channelID,
			After: int64(pg.After), Lim: int32(lim)}) //nolint:gosec // G115: range checked
	} else {
		rows, err = s.q.MessagesBefore(ctx, s.pool, store.MessagesBeforeParams{ChannelID: channelID,
			Before: int64(pg.Before), Lim: int32(lim)}) //nolint:gosec // G115: range checked
	}
	if err != nil {
		return nil, fmt.Errorf("messages: list messages: %w", err)
	}
	return s.complete(ctx, guildID, channelID, rows, userID)
}

// complete adds the authors and the replied-to messages to rows of one
// channel, and batches loading the page's attachments and reaction summaries.
func (s *Service) complete(ctx context.Context, guildID, channelID snowflake.ID, rows []store.Message, caller snowflake.ID) ([]Message, error) {
	var replyIDs []int64
	seen := map[snowflake.ID]bool{}
	for _, r := range rows {
		if r.ReplyToID != nil && !seen[*r.ReplyToID] {
			seen[*r.ReplyToID] = true
			replyIDs = append(replyIDs, int64(*r.ReplyToID))
		}
	}
	originals := map[snowflake.ID]store.Message{}
	if len(replyIDs) > 0 {
		refs, err := s.q.MessagesByIDs(ctx, s.pool, store.MessagesByIDsParams{ChannelID: channelID, Ids: replyIDs})
		if err != nil {
			return nil, fmt.Errorf("messages: load replied messages: %w", err)
		}
		for _, r := range refs {
			originals[r.ID] = r
		}
	}

	var authorIDs []snowflake.ID
	wanted := map[snowflake.ID]bool{}
	addAuthor := func(id *snowflake.ID) {
		if id != nil && !wanted[*id] {
			wanted[*id] = true
			authorIDs = append(authorIDs, *id)
		}
	}
	for _, r := range rows {
		addAuthor(r.AuthorID)
	}
	for _, r := range originals {
		addAuthor(r.AuthorID)
	}
	users, err := s.users.UsersByIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[snowflake.ID]*auth.User, len(users))
	for i := range users {
		byID[users[i].ID] = &users[i]
	}
	author := func(id *snowflake.ID) *auth.User {
		if id == nil {
			return nil
		}
		return byID[*id]
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.IsZero() {
			ids = append(ids, int64(row.ID))
		}
	}
	byMessage := make(map[snowflake.ID][]Attachment)
	var reactions map[snowflake.ID][]Reaction
	if len(ids) > 0 {
		attachments, err := s.q.AttachmentsByMessages(ctx, s.pool, ids)
		if err != nil {
			return nil, fmt.Errorf("messages: load attachments: %w", err)
		}
		reactions, err = s.reactionSummaries(ctx, ids, caller)
		if err != nil {
			return nil, fmt.Errorf("messages: complete message page: %w", err)
		}
		for _, file := range attachments {
			id := snowflake.ID(file.MessageID.Int64)
			byMessage[id] = append(byMessage[id], attachment(file))
		}
	}

	out := make([]Message, len(rows))
	for i, r := range rows {
		embeds, err := decodeEmbeds(r.Embeds)
		if err != nil {
			return nil, err
		}
		m := Message{ID: r.ID, ChannelID: r.ChannelID, GuildID: guildID, Author: author(r.AuthorID),
			Content: r.Content, CreatedAt: r.CreatedAt, EditedAt: r.EditedAt, ReplyToID: r.ReplyToID, Attachments: byMessage[r.ID], Embeds: embeds, Reactions: reactions[r.ID], mentions: storedMentions(r)}
		if r.ReplyToID != nil {
			if o, ok := originals[*r.ReplyToID]; ok {
				m.Referenced = &Stub{ID: o.ID, Author: author(o.AuthorID), Content: o.Content}
			}
		}
		out[i] = m
	}
	return out, nil
}

func (s *Service) completeOne(ctx context.Context, guildID snowflake.ID, row store.Message, caller snowflake.ID) (Message, error) {
	ms, err := s.complete(ctx, guildID, row.ChannelID, []store.Message{row}, caller)
	if err != nil {
		return Message{}, err
	}
	return ms[0], nil
}

// CreateFields is a new message.
type CreateFields struct {
	Content       string
	ReplyToID     *snowflake.ID
	Nonce         *string
	AttachmentIDs []snowflake.ID
}

// Create sends a message to a text channel; userID needs SEND_MESSAGES
// there, and READ_MESSAGE_HISTORY for a reply, which must answer a message
// of the same channel. The message is announced as MESSAGE_CREATE with the
// nonce. A nonce is remembered for NonceTTL per author and channel: sending
// with it again returns the message already sent, announced only the first
// time.
func (s *Service) Create(ctx context.Context, channelID, userID snowflake.ID, f CreateFields) (Message, error) {
	p, guildID, err := s.access(ctx, userID, channelID, permissions.SendMessages)
	if err != nil {
		return Message{}, err
	}
	// A reply shows the original: only for those who may read the history,
	// checked before its existence so the answer cannot probe for messages.
	if f.ReplyToID != nil && !p.Has(permissions.ReadMessageHistory) {
		return Message{}, ErrMissingPerm
	}
	if err = s.requireText(ctx, channelID); err != nil {
		return Message{}, err
	}
	if len(f.AttachmentIDs) > MaxAttachments {
		return Message{}, attachmentFieldProblem("too_many_attachments", fmt.Sprintf("must contain at most %d IDs", MaxAttachments))
	}
	if len(f.AttachmentIDs) > 0 && !p.Has(permissions.AttachFiles) {
		return Message{}, ErrMissingPerm
	}
	content := ""
	if len(f.AttachmentIDs) == 0 || strings.TrimSpace(f.Content) != "" {
		if content, err = NormalizeContent(f.Content); err != nil {
			return Message{}, err
		}
	}
	if f.Nonce != nil && utf8.RuneCountInString(*f.Nonce) > MaxNonce {
		return Message{}, invalid("nonce", fmt.Sprintf("must be at most %d characters", MaxNonce))
	}
	if f.ReplyToID != nil {
		_, err = s.q.MessageInChannel(ctx, s.pool, store.MessageInChannelParams{ID: *f.ReplyToID, ChannelID: channelID})
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, invalid("reply_to_id", "must be a message of this channel")
		}
		if err != nil {
			return Message{}, fmt.Errorf("messages: load replied message: %w", err)
		}
	}
	params := store.CreateMessageParams{ChannelID: channelID, AuthorID: &userID,
		Content: content, ReplyToID: f.ReplyToID}
	// Everything the event shows but created_at is read before the write,
	// so nothing can fail between the write and the event.
	m, err := s.completeOne(ctx, guildID, store.Message{ID: params.ID, ChannelID: channelID, AuthorID: &userID,
		Content: content, ReplyToID: f.ReplyToID}, userID)
	if err != nil {
		return Message{}, err
	}
	var row store.Message
	var picked previewSelection
	// A claim that disappears before it is read frees the nonce for one retry.
	for range 2 {
		if row, err = s.createOnce(ctx, guildID, params, f, &m, &picked); !errors.Is(err, errNonceTaken) {
			break
		}
		if sent, ok, sentErr := s.sentWith(ctx, guildID, channelID, userID, *f.Nonce); ok || sentErr != nil {
			return sent, sentErr
		}
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
		return Message{}, channels.ErrNotFound // the channel went away meanwhile
	}
	if err != nil {
		return Message{}, fmt.Errorf("messages: create message: %w", err)
	}
	m.ID, m.CreatedAt, m.mentions = row.ID, row.CreatedAt, storedMentions(row)
	m.Nonce = f.Nonce
	m.Embeds = picked.embeds
	s.publish(ctx, events.Event{Type: events.MessageCreate, Audience: channelAudience(guildID, channelID),
		Payload: m.Public()})
	s.enqueuePreviews(m, picked.pending)
	return m, nil
}

// errNonceTaken: a live claim holds the nonce, so its message was sent.
var errNonceTaken = errors.New("messages: nonce taken")

// createOnce stores the message, nonce, files and unread state in one transaction.
func (s *Service) createOnce(ctx context.Context, guildID snowflake.ID, params store.CreateMessageParams, f CreateFields, m *Message, picked *previewSelection) (store.Message, error) {
	var row store.Message
	err := s.withGuildWrite(ctx, guildID, func(tx pgx.Tx) error {
		p, _, err := s.channels.PermissionsIn(ctx, tx, *params.AuthorID, params.ChannelID)
		if err != nil {
			return fmt.Errorf("messages: recheck send access: %w", err)
		}
		need := permissions.SendMessages
		if f.ReplyToID != nil {
			need |= permissions.ReadMessageHistory
		}
		if len(f.AttachmentIDs) > 0 {
			need |= permissions.AttachFiles
		}
		if !p.Has(need) {
			return ErrMissingPerm
		}
		resolved, err := s.resolveMentions(ctx, tx, params.ChannelID, *params.AuthorID, p, params.Content)
		if err != nil {
			return err
		}
		// IDs are assigned inside the write lock so read positions and send
		// order agree even when two requests reach the database together.
		params.ID = s.ids.Next()
		params.MentionUserIds, params.MentionRoleIds = resolved.userIDs, resolved.roleIDs
		params.MentionEveryone, params.MentionHere = resolved.everyone, resolved.here
		*picked, err = s.selectPreviews(ctx, tx, params.Content, nil)
		if err != nil {
			return err
		}
		params.Embeds, err = encodeEmbeds(picked.embeds)
		if err != nil {
			return err
		}
		if row, err = s.q.CreateMessage(ctx, tx, params); err != nil {
			return fmt.Errorf("messages: insert message: %w", err)
		}
		if f.Nonce != nil {
			if err = s.claimNonce(ctx, tx, params, *f.Nonce); err != nil {
				return err
			}
		}
		if len(f.AttachmentIDs) > 0 {
			m.Attachments, err = s.bindAttachments(ctx, tx, params, f.AttachmentIDs)
			if err != nil {
				return err
			}
		}
		return s.recordUnread(ctx, tx, guildID, params, resolved)
	})
	return row, err
}

// recordUnread updates the channel marker, sender marker and recipients after a new send.
func (s *Service) recordUnread(ctx context.Context, tx pgx.Tx, guildID snowflake.ID, msg store.CreateMessageParams, m mentions) error {
	channelID, messageID := int64(msg.ChannelID), int64(msg.ID)
	if err := s.q.SetLastMessage(ctx, tx, store.SetLastMessageParams{ChannelID: channelID, MessageID: messageID}); err != nil {
		return fmt.Errorf("messages: set newest message: %w", err)
	}
	if _, err := s.advanceRead(ctx, tx, guildID, *msg.AuthorID, []int64{channelID}, []int64{messageID}); err != nil {
		return err
	}
	if len(m.recipients) == 0 {
		return nil
	}
	if err := s.saveMentionRecipients(ctx, tx, msg.ID, m); err != nil {
		return err
	}
	if err := s.q.IncrementMentions(ctx, tx, store.IncrementMentionsParams{GuildID: int64(guildID), ChannelID: channelID,
		MessageID: messageID, UserIds: m.recipients}); err != nil {
		return fmt.Errorf("messages: increment mention counters: %w", err)
	}
	return nil
}

// claimNonce records a send's retry key inside its write transaction.
func (s *Service) claimNonce(ctx context.Context, tx pgx.Tx, params store.CreateMessageParams, nonce string) error {
	if err := s.q.DropExpiredNonces(ctx, tx, *params.AuthorID); err != nil {
		return fmt.Errorf("messages: drop expired nonces: %w", err)
	}
	n, err := s.q.ClaimNonce(ctx, tx, store.ClaimNonceParams{AuthorID: *params.AuthorID,
		ChannelID: params.ChannelID, Nonce: nonce, MessageID: params.ID, TtlSeconds: NonceTTL.Seconds()})
	if err != nil {
		return fmt.Errorf("messages: claim nonce: %w", err)
	}
	if n == 0 {
		return errNonceTaken
	}
	return nil
}

// attachmentFieldProblem reports an invalid selection without identifying its owner.
func attachmentFieldProblem(code, detail string) error {
	return httpx.BadRequest(code, "some fields are invalid", httpx.FieldError{Field: "attachment_ids", Message: detail})
}

// bindAttachments updates only eligible rows, and rolls back the send if
// any requested ID could not be bound (including duplicate IDs).
func (s *Service) bindAttachments(ctx context.Context, tx pgx.Tx, params store.CreateMessageParams, ids []snowflake.ID) ([]Attachment, error) {
	keys := make([]int64, len(ids))
	for i, id := range ids {
		keys[i] = int64(id)
	}
	rows, err := s.q.BindAttachments(ctx, tx, store.BindAttachmentsParams{MessageID: dbID(params.ID),
		ChannelID: int64(params.ChannelID), UploaderID: dbID(*params.AuthorID), Cutoff: s.now().Add(-PendingTTL), Ids: keys})
	if err != nil {
		return nil, fmt.Errorf("messages: bind attachments: %w", err)
	}
	if len(rows) != len(keys) {
		return nil, attachmentFieldProblem("attachment_not_found", "must contain distinct pending attachment IDs from this channel")
	}
	slices.SortFunc(rows, func(a, b store.MessageAttachment) int { return int(a.Position - b.Position) })
	out := make([]Attachment, len(rows))
	for i, row := range rows {
		out[i] = attachment(row)
	}
	return out, nil
}

// sentWith returns the message userID sent to the channel with the nonce,
// for a retry (it is not announced again); false when no live claim holds
// the nonce.
func (s *Service) sentWith(ctx context.Context, guildID, channelID, userID snowflake.ID, nonce string) (Message, bool, error) {
	row, err := s.q.MessageByNonce(ctx, s.pool, store.MessageByNonceParams{AuthorID: userID, ChannelID: channelID,
		Nonce: nonce})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, fmt.Errorf("messages: load message by nonce: %w", err)
	}
	m, err := s.completeOne(ctx, guildID, row, userID)
	if err != nil {
		return Message{}, false, err
	}
	m.Nonce = &nonce
	return m, true, nil
}

func (s *Service) load(ctx context.Context, channelID, messageID snowflake.ID) (store.Message, error) {
	row, err := s.q.MessageInChannel(ctx, s.pool, store.MessageInChannelParams{ID: messageID, ChannelID: channelID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Message{}, ErrNotFound
	}
	if err != nil {
		return store.Message{}, fmt.Errorf("messages: load message: %w", err)
	}
	return row, nil
}

func authoredBy(row store.Message, userID snowflake.ID) bool {
	return row.AuthorID != nil && *row.AuthorID == userID
}

// loadFor loads a message for userID, who holds p in the channel. Without
// READ_MESSAGE_HISTORY only their own messages exist for them, so a missing
// message and someone else's look the same (ErrNotFound).
func (s *Service) loadFor(ctx context.Context, p permissions.Permission, channelID, messageID, userID snowflake.ID) (store.Message, error) {
	row, err := s.load(ctx, channelID, messageID)
	if err != nil {
		return store.Message{}, err
	}
	if !authoredBy(row, userID) && !p.Has(permissions.ReadMessageHistory) {
		return store.Message{}, ErrNotFound
	}
	return row, nil
}

// Edit replaces the content of userID's own message; edited_at comes from
// the database clock. The same content again changes nothing and announces
// nothing; a change is announced as MESSAGE_UPDATE. Without
// READ_MESSAGE_HISTORY someone else's message is ErrNotFound.
func (s *Service) Edit(ctx context.Context, channelID, messageID, userID snowflake.ID, content string) (Message, error) {
	p, guildID, err := s.access(ctx, userID, channelID, permissions.ViewChannel)
	if err != nil {
		return Message{}, err
	}
	defer s.lockPreviewUpdate(messageID)()
	row, err := s.loadFor(ctx, p, channelID, messageID, userID)
	if err != nil {
		return Message{}, err
	}
	if !authoredBy(row, userID) {
		return Message{}, ErrMissingPerm
	}
	if content, err = NormalizeContent(content); err != nil {
		return Message{}, err
	}
	// Everything the event shows but the edit is read before the write, so
	// nothing can fail between the write and the event.
	m, err := s.completeOne(ctx, guildID, row, userID)
	if err != nil || content == row.Content {
		return m, err
	}
	var picked previewSelection
	edited, err := s.editOnce(ctx, guildID, channelID, messageID, userID, content, &picked)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted, or a concurrent edit set this very content: report what
		// is there now without announcing anything.
		if row, err = s.load(ctx, channelID, messageID); err != nil {
			return Message{}, err
		}
		return s.completeOne(ctx, guildID, row, userID)
	}
	if err != nil {
		return Message{}, fmt.Errorf("messages: edit message: %w", err)
	}
	m.Content, m.EditedAt, m.mentions = edited.Content, edited.EditedAt, storedMentions(edited)
	m.Embeds = picked.embeds
	s.publishMessageUpdate(ctx, m)
	s.enqueuePreviews(m, picked.pending)
	return m, nil
}

// publishMessageUpdate emits a shared payload without caller-specific reactions.
func (s *Service) publishMessageUpdate(ctx context.Context, m Message) {
	update := m.Public()
	update.Reactions = nil
	s.publish(ctx, events.Event{Type: events.MessageUpdate, Audience: channelAudience(m.GuildID, m.ChannelID), Payload: update})
}

// editOnce updates syntax and the recipients used on the next acknowledgment.
func (s *Service) editOnce(ctx context.Context, guildID, channelID, messageID, userID snowflake.ID, content string, picked *previewSelection) (store.Message, error) {
	var edited store.Message
	err := s.withGuildWrite(ctx, guildID, func(tx pgx.Tx) error {
		p, _, err := s.channels.PermissionsIn(ctx, tx, userID, channelID)
		if err != nil {
			return fmt.Errorf("messages: recheck edit access: %w", err)
		}
		m, err := s.resolveMentions(ctx, tx, channelID, userID, p, content)
		if err != nil {
			return err
		}
		current, err := s.q.MessageInChannel(ctx, tx, store.MessageInChannelParams{ID: messageID, ChannelID: channelID})
		if err != nil {
			return fmt.Errorf("messages: read current embeds: %w", err)
		}
		*picked, err = s.selectPreviews(ctx, tx, content, current.Embeds)
		if err != nil {
			return err
		}
		data, err := encodeEmbeds(picked.embeds)
		if err != nil {
			return err
		}
		edited, err = s.q.EditMessage(ctx, tx, store.EditMessageParams{Content: content, ID: messageID, Embeds: data,
			ChannelID: channelID, AuthorID: &userID, MentionUserIds: m.userIDs, MentionRoleIds: m.roleIDs,
			MentionEveryone: m.everyone, MentionHere: m.here})
		if err != nil {
			return fmt.Errorf("messages: update content: %w", err)
		}
		if err = s.q.ClearMentionRecipients(ctx, tx, int64(messageID)); err != nil {
			return fmt.Errorf("messages: clear old mention recipients: %w", err)
		}
		return s.saveMentionRecipients(ctx, tx, messageID, m)
	})
	return edited, err
}

// Delete deletes a message; its author or a member with MANAGE_MESSAGES in
// the channel may (one without READ_MESSAGE_HISTORY gets ErrNotFound for
// someone else's). Announced as MESSAGE_DELETE.
func (s *Service) Delete(ctx context.Context, channelID, messageID, userID snowflake.ID) error {
	p, guildID, err := s.access(ctx, userID, channelID, permissions.ViewChannel)
	if err != nil {
		return err
	}
	defer s.lockPreviewUpdate(messageID)()
	row, err := s.loadFor(ctx, p, channelID, messageID, userID)
	if err != nil {
		return err
	}
	if !authoredBy(row, userID) && !p.Has(permissions.ManageMessages) {
		return ErrMissingPerm
	}
	var last *snowflake.ID
	err = s.withGuildWrite(ctx, guildID, func(tx pgx.Tx) error {
		current, _, accessErr := s.channels.PermissionsIn(ctx, tx, userID, channelID)
		if accessErr != nil {
			return fmt.Errorf("messages: recheck delete access: %w", accessErr)
		}
		if !authoredBy(row, userID) {
			if !current.Has(permissions.ReadMessageHistory) {
				return ErrNotFound
			}
			if !current.Has(permissions.ManageMessages) {
				return ErrMissingPerm
			}
		}
		n, deleteErr := s.q.DeleteMessage(ctx, tx, store.DeleteMessageParams{ID: messageID, ChannelID: channelID})
		if deleteErr != nil {
			return fmt.Errorf("messages: delete message: %w", deleteErr)
		}
		if n == 0 {
			return ErrNotFound
		}
		marker, recomputeErr := s.q.RecomputeLastMessage(ctx, tx, channelID)
		if recomputeErr != nil {
			return fmt.Errorf("messages: recompute newest message: %w", recomputeErr)
		}
		if marker.Valid {
			id := snowflake.ID(marker.Int64)
			last = &id
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("messages: delete transaction: %w", err)
	}
	s.tryAttachmentTrash(ctx)
	s.publish(ctx, events.Event{Type: events.MessageDelete, Audience: channelAudience(guildID, channelID),
		Payload: DeletePayload{ID: messageID, ChannelID: channelID, GuildID: guildID, LastMessageID: last}})
	return nil
}

// Typing announces that userID is typing in a text channel to everyone else
// who can view it; userID needs SEND_MESSAGES there. Calls within
// TypingInterval of the last announced one for the same user and channel
// succeed silently, before the channel type is even checked.
func (s *Service) Typing(ctx context.Context, channelID, userID snowflake.ID) error {
	_, guildID, err := s.access(ctx, userID, channelID, permissions.SendMessages)
	if err != nil {
		return err
	}
	// Throttled before the channel type is read, so repeats cost no query.
	if ok, _ := s.typingLimiter.Allow(userID.String() + ":" + channelID.String()); !ok {
		return nil
	}
	if err = s.requireText(ctx, channelID); err != nil {
		return err
	}
	s.publish(ctx, events.Event{Type: events.TypingStart, Audience: channelAudience(guildID, channelID),
		ExcludeUserID: userID,
		Payload: TypingPayload{ChannelID: channelID, GuildID: guildID, UserID: userID,
			Timestamp: httpx.FormatTime(s.now())}})
	return nil
}
