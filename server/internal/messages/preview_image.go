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

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	// Preview files use the attachment store's reserved owner outside guild channels.
	embedImageOwner  snowflake.ID = 1
	embedImagePrefix              = "/api/v1/embeds/"
	embedTokenLength              = 26
)

// saveEmbedImage stores validated originals with an unguessable public URL.
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
	if len(token) != embedTokenLength {
		return httpx.NotFound()
	}
	image, err := s.q.EmbedImageByToken(r.Context(), s.pool, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("messages: look up preview image: %w", err)
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
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, token, info.ModTime(), f)
	return nil
}
