package guilds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

const (
	maxGuildEmojisPerKind = 50
	maxEmojiName          = 32
	emojiURLPrefix        = "/api/v1/emojis/"
)

// emojiWriteTimeout bounds an emoji upload's transaction and image save,
// which hold the guild's lock. A test binary gets a minute: a CI host that
// runs many packages at once can stall a write well past fifteen seconds.
var emojiWriteTimeout = func() time.Duration {
	if testing.Testing() {
		return time.Minute
	}
	return 15 * time.Second
}()

var emojiName = regexp.MustCompile(`^[A-Za-z0-9_]{2,32}$`)

// ValidEmojiName reports whether name uses the allowed custom emoji syntax.
func ValidEmojiName(name string) bool { return emojiName.MatchString(name) }

// Emoji is a server-owned image and its public metadata.
type Emoji struct {
	ID        snowflake.ID  `json:"id"`
	GuildID   snowflake.ID  `json:"guild_id"`
	Name      string        `json:"name"`
	Animated  bool          `json:"animated"`
	URL       string        `json:"url"`
	CreatorID *snowflake.ID `json:"creator_id"`
	CreatedAt string        `json:"created_at"`
}

// EmojisUpdate gives members the full current list after a write.
type EmojisUpdate struct {
	GuildID snowflake.ID `json:"guild_id"`
	Emojis  []Emoji      `json:"emojis"`
}

// publicEmoji builds the same representation for HTTP and the gateway.
func publicEmoji(row store.GuildEmoji) Emoji {
	id := snowflake.ID(row.ID)
	e := Emoji{ID: id, GuildID: snowflake.ID(row.GuildID), Name: row.Name, Animated: row.Animated,
		URL: emojiURLPrefix + id.String(), CreatedAt: httpx.FormatTime(row.CreatedAt)}
	if row.CreatorID.Valid {
		creator := snowflake.ID(row.CreatorID.Int64)
		e.CreatorID = &creator
	}
	return e
}

// emojiList reads active emoji in creation order and always returns an array.
func (s *Service) emojiList(ctx context.Context, q store.DBTX, guildID snowflake.ID) ([]Emoji, error) {
	rows, err := s.q.ListGuildEmojis(ctx, q, int64(guildID))
	if err != nil {
		return nil, fmt.Errorf("guilds: list emoji: %w", err)
	}
	out := make([]Emoji, len(rows))
	for i, row := range rows {
		out[i] = publicEmoji(row)
	}
	return out, nil
}

// ListEmojis reads the guild's emoji at the same snapshot as the membership check.
func (s *Service) ListEmojis(ctx context.Context, guildID, userID snowflake.ID) ([]Emoji, error) {
	var out []Emoji
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		ok, err := s.q.IsMember(ctx, tx, store.IsMemberParams{GuildID: guildID, UserID: userID})
		if err != nil {
			return fmt.Errorf("guilds: check emoji list access: %w", err)
		}
		if !ok {
			return ErrNotMember
		}
		out, err = s.emojiList(ctx, tx, guildID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("guilds: read emoji list: %w", err)
	}
	return out, nil
}

// CreateEmoji validates an image and adds it under the guild's permission
// and capacity locks. The same ID is used if the database retries a write.
func (s *Service) CreateEmoji(ctx context.Context, guildID, actor snowflake.ID, name string, raw []byte) (Emoji, error) {
	if err := s.checkManageExpressions(ctx, guildID, actor); err != nil {
		return Emoji{}, fmt.Errorf("guilds: authorize emoji upload: %w", err)
	}
	if !ValidEmojiName(name) {
		return Emoji{}, invalid("name", "must be 2 to 32 ASCII letters, digits or underscores")
	}
	if len(raw) > files.MaxEmojiBytes {
		return Emoji{}, upload.TooLarge(files.MaxEmojiBytes)
	}
	if s.emojiFiles == nil {
		return Emoji{}, httpx.ServiceUnavailableRetryAfter("server_busy", 0)
	}
	data, animated, err := s.emojiFiles.PrepareEmoji(ctx, raw)
	if errors.Is(err, files.ErrUnsupportedImage) {
		return Emoji{}, httpx.BadRequest("unsupported_image", "use a PNG, JPEG, WebP or GIF image. Animated GIFs must fit within 128 by 128 pixels")
	}
	if err != nil {
		return Emoji{}, fmt.Errorf("guilds: prepare emoji: %w", err)
	}
	id := s.ids.Next()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Emoji{}, fmt.Errorf("guilds: finish emoji upload: %w", ctxErr)
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emojiWriteTimeout)
	defer cancel()
	return s.writeEmoji(writeCtx, guildID, actor, data, func(tx pgx.Tx) (store.GuildEmoji, bool, error) {
		n, countErr := s.q.CountGuildEmojis(writeCtx, tx, store.CountGuildEmojisParams{GuildID: int64(guildID), Animated: animated})
		if countErr != nil {
			return store.GuildEmoji{}, false, fmt.Errorf("guilds: count emoji: %w", countErr)
		}
		if n >= maxGuildEmojisPerKind {
			kind := "static"
			if animated {
				kind = "animated"
			}
			return store.GuildEmoji{}, false, httpx.BadRequest("too_many_emojis", fmt.Sprintf("a server can have at most %d %s emoji", maxGuildEmojisPerKind, kind))
		}
		row, createErr := s.q.CreateGuildEmoji(writeCtx, tx, store.CreateGuildEmojiParams{ID: int64(id), GuildID: int64(guildID),
			Name: name, Animated: animated, CreatorID: pgtype.Int8{Int64: int64(actor), Valid: true}, CreatedAt: s.now()})
		if createErr != nil {
			return store.GuildEmoji{}, false, fmt.Errorf("guilds: insert emoji: %w", createErr)
		}
		if recErr := s.record(writeCtx, tx, audit.Entry{GuildID: guildID, ActorID: actor, Action: audit.EmojiCreate,
			TargetID: id, Changes: []audit.Change{{Key: "name", New: name}}}); recErr != nil {
			return store.GuildEmoji{}, false, recErr
		}
		return row, true, nil
	})
}

// RenameEmoji changes an active emoji's display name without changing its identity.
func (s *Service) RenameEmoji(ctx context.Context, guildID, actor, id snowflake.ID, name string) (Emoji, error) {
	return s.writeEmoji(ctx, guildID, actor, nil, func(tx pgx.Tx) (store.GuildEmoji, bool, error) {
		if !ValidEmojiName(name) {
			return store.GuildEmoji{}, false, invalid("name", "must be 2 to 32 ASCII letters, digits or underscores")
		}
		row, err := s.q.ActiveGuildEmoji(ctx, tx, store.ActiveGuildEmojiParams{ID: int64(id), GuildID: int64(guildID)})
		if err != nil {
			return store.GuildEmoji{}, false, fmt.Errorf("guilds: load emoji for rename: %w", err)
		}
		if row.Name == name {
			return row, false, nil
		}
		old := row.Name
		row, err = s.q.RenameGuildEmoji(ctx, tx, store.RenameGuildEmojiParams{ID: int64(id), GuildID: int64(guildID), Name: name})
		if err != nil {
			return store.GuildEmoji{}, false, fmt.Errorf("guilds: rename emoji: %w", err)
		}
		if err = s.record(ctx, tx, audit.Entry{GuildID: guildID, ActorID: actor, Action: audit.EmojiUpdate,
			TargetID: id, Changes: []audit.Change{{Key: "name", Old: old, New: name}}}); err != nil {
			return store.GuildEmoji{}, false, err
		}
		return row, true, nil
	})
}

// DeleteEmoji removes an emoji from the picker and queues its image for
// trash. The row retains the last name for reactions already using it.
func (s *Service) DeleteEmoji(ctx context.Context, guildID, actor, id snowflake.ID) error {
	_, err := s.writeEmoji(ctx, guildID, actor, nil, func(tx pgx.Tx) (store.GuildEmoji, bool, error) {
		row, deleteErr := s.q.DeleteGuildEmoji(ctx, tx, store.DeleteGuildEmojiParams{ID: int64(id), GuildID: int64(guildID)})
		if deleteErr != nil {
			return store.GuildEmoji{}, false, fmt.Errorf("guilds: delete emoji: %w", deleteErr)
		}
		if recErr := s.record(ctx, tx, audit.Entry{GuildID: guildID, ActorID: actor, Action: audit.EmojiDelete,
			TargetID: id, Changes: []audit.Change{{Key: "name", Old: row.Name}}}); recErr != nil {
			return store.GuildEmoji{}, false, recErr
		}
		return row, true, nil
	})
	if err != nil {
		return fmt.Errorf("guilds: remove emoji: %w", err)
	}
	s.dropEmoji(ctx, id)
	return nil
}

// writeEmoji serializes permissions, capacity and update publication with
// other emoji writes and guild deletion.
func (s *Service) writeEmoji(ctx context.Context, guildID, actor snowflake.ID, image []byte, change func(pgx.Tx) (store.GuildEmoji, bool, error)) (Emoji, error) {
	mu := s.iconLock(guildID)
	mu.Lock()
	defer mu.Unlock()
	var row store.GuildEmoji
	var list []Emoji
	var storedID snowflake.ID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockGuild(ctx, tx, guildID); err != nil {
			return err
		}
		if _, err := s.require(ctx, tx, guildID, actor, permissions.ManageExpressions); err != nil {
			return err
		}
		next, changed, err := change(tx)
		row, list = next, nil
		if err != nil || !changed {
			return err
		}
		list, err = s.emojiList(ctx, tx, guildID)
		if err != nil || image == nil {
			return err
		}
		// Save last. A process crash or unknown commit outcome leaves the
		// original for reconcile rather than risking a live reference.
		id := snowflake.ID(row.ID)
		if _, saveErr := s.emojiFiles.SaveAttachment(ctx, files.EmojiReference(id), bytes.NewReader(image)); saveErr != nil {
			return fmt.Errorf("guilds: save emoji image: %w", saveErr)
		}
		storedID = id
		return nil
	})
	if storedID != 0 && confirmedEmojiRollback(err) {
		s.dropEmoji(ctx, storedID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Emoji{}, ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.ConstraintName == "guild_emojis_active_name_idx" {
		return Emoji{}, invalid("name", "an emoji already uses this name")
	}
	if err != nil {
		return Emoji{}, fmt.Errorf("guilds: write emoji: %w", err)
	}
	if list != nil {
		s.publish(ctx, events.Event{Type: events.GuildEmojisUpdate,
			Audience: events.Audience{Kind: events.AudienceGuild, GuildID: guildID},
			Payload:  EmojisUpdate{GuildID: guildID, Emojis: list}})
	}
	return publicEmoji(row), nil
}

// confirmedEmojiRollback identifies a database rejection that cannot have committed.
func confirmedEmojiRollback(err error) bool {
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return true
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && (strings.HasPrefix(pgErr.Code, "23") || pgErr.Code == "40001" || pgErr.Code == "40P01")
}

// checkManageExpressions refuses unauthorized uploads before reading a body.
func (s *Service) checkManageExpressions(ctx context.Context, guildID, actor snowflake.ID) error {
	_, err := s.require(ctx, s.pool, guildID, actor, permissions.ManageExpressions)
	return err
}

// dropEmoji attempts the durable trash job immediately after commit.
func (s *Service) dropEmoji(ctx context.Context, id snowflake.ID) {
	if s.emojiFiles == nil {
		return
	}
	if err := s.emojiFiles.DeleteAttachment(files.EmojiReference(id)); err != nil {
		httpx.LoggerFrom(ctx).WarnContext(ctx, "trash emoji image", "emoji_id", id, "err", err)
	}
}
