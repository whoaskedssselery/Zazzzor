package guilds

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

// handleListEmojis returns the picker list to a guild member.
func (s *Service) handleListEmojis(w http.ResponseWriter, r *http.Request) error {
	guild, err := httpx.PathID(r, "guild_id")
	actor := auth.CurrentUser(r).ID
	if err != nil {
		return err
	}
	list, err := s.ListEmojis(r.Context(), guild, actor)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

// handleCreateEmoji shares the icon upload gate and rejects unauthorized bodies.
func (s *Service) handleCreateEmoji(w http.ResponseWriter, r *http.Request) error {
	guild, err := httpx.PathID(r, "guild_id")
	actor := auth.CurrentUser(r).ID
	if err != nil {
		return err
	}
	if err = s.checkManageExpressions(r.Context(), guild, actor); err != nil {
		return fail(err)
	}
	upload.ExtendDeadlines(w)
	name, data, err := upload.ReadNamedFile(w, r, files.MaxEmojiBytes, maxEmojiName)
	if err != nil {
		return err
	}
	emoji, err := s.CreateEmoji(r.Context(), guild, actor, name, data)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, emoji)
	return nil
}

type renameEmojiRequest struct {
	Name string `json:"name"`
}

// handleRenameEmoji renames an active emoji and returns its updated metadata.
func (s *Service) handleRenameEmoji(w http.ResponseWriter, r *http.Request) error {
	guild, err := httpx.PathID(r, "guild_id")
	actor := auth.CurrentUser(r).ID
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "emoji_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[renameEmojiRequest](r)
	if err != nil {
		return err
	}
	emoji, err := s.RenameEmoji(r.Context(), guild, actor, id, req.Name)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, emoji)
	return nil
}

// handleDeleteEmoji deletes an active emoji from the server.
func (s *Service) handleDeleteEmoji(w http.ResponseWriter, r *http.Request) error {
	guild, err := httpx.PathID(r, "guild_id")
	actor := auth.CurrentUser(r).ID
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "emoji_id")
	if err != nil {
		return err
	}
	if err = s.DeleteEmoji(r.Context(), guild, actor, id); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleEmojiImage checks membership before opening the private image.
func (s *Service) handleEmojiImage(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Cookie")
	actor, ok := auth.FromContext(r.Context())
	if !ok || s.emojiFiles == nil {
		return httpx.NotFound()
	}
	id, err := httpx.PathID(r, "emoji_id")
	if err != nil {
		return httpx.NotFound()
	}
	row, err := s.q.GuildEmojiForMember(r.Context(), s.pool, store.GuildEmojiForMemberParams{ID: int64(id), UserID: int64(actor.ID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("guilds: authorize emoji image: %w", err)
	}
	file, err := s.emojiFiles.OpenAttachment(files.EmojiReference(id))
	if errors.Is(err, os.ErrNotExist) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("guilds: open emoji image: %w", err)
	}
	defer func() { _ = file.Close() }()
	mime := "image/png"
	if row.Animated {
		mime = "image/gif"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, id.String(), row.CreatedAt, file)
	return nil
}
