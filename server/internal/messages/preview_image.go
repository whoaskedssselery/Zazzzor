package messages

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	// Preview files use the attachment store's reserved owner outside guild channels.
	embedImageOwner  snowflake.ID = 1
	embedImagePrefix              = "/api/v1/embeds/"
	embedTokenLength              = 26
)

// saveEmbedImage stores validated originals behind an unguessable URL.
func (s *Service) saveEmbedImage(ctx context.Context, data []byte, contentType string) (string, error) {
	id, token := s.ids.Next(), rand.Text()
	if err := s.q.AddEmbedImage(ctx, s.pool, store.AddEmbedImageParams{
		ID: int64(id), Token: token, ContentType: contentType, CreatedAt: s.now(),
	}); err != nil {
		return "", fmt.Errorf("messages: register preview image: %w", err)
	}
	if _, err := s.files.SaveAttachment(ctx, files.AttachmentReference(embedImageOwner, id), bytes.NewReader(data)); err != nil {
		s.logger.Warn("write preview image", "id", id, "err", err)
		return "", fmt.Errorf("messages: store preview image: %w", err)
	}
	return embedImagePrefix + token, nil
}

// handleEmbedImage serves only validated raster originals from the local disk.
func (s *Service) handleEmbedImage(w http.ResponseWriter, r *http.Request) error {
	token := chi.URLParam(r, "embed_image_id")
	user := auth.CurrentUser(r).ID
	if user == 0 || len(token) != embedTokenLength {
		return httpx.NotFound()
	}
	ctx := r.Context()
	image, err := s.q.EmbedImageByToken(ctx, s.pool, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("messages: look up preview image: %w", err)
	}
	if err = s.checkEmbedImageAccess(ctx, user, embedImagePrefix+token); err != nil {
		return err
	}
	f, err := s.files.OpenAttachment(files.AttachmentReference(embedImageOwner, snowflake.ID(image.ID)))
	if errors.Is(err, os.ErrNotExist) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("messages: open preview image: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("messages: stat preview image: %w", err)
	}
	w.Header().Set("Content-Type", image.ContentType)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Add("Vary", "Cookie")
	w.Header().Set("ETag", `"`+token+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, token, info.ModTime(), f)
	return nil
}

// checkEmbedImageAccess requires at least one message the reader can access.
func (s *Service) checkEmbedImageAccess(ctx context.Context, user snowflake.ID, imageURL string) error {
	ids, err := s.q.EmbedImageChannels(ctx, s.pool, imageURL)
	if err != nil {
		return fmt.Errorf("messages: load preview image channels: %w", err)
	}
	for _, id := range ids {
		_, _, err := s.access(ctx, user, id, permissions.ViewChannel|permissions.ReadMessageHistory)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, channels.ErrNotFound), errors.Is(err, ErrMissingPerm):
			continue
		default:
			return fmt.Errorf("messages: check preview image access: %w", err)
		}
	}
	return httpx.NotFound()
}
