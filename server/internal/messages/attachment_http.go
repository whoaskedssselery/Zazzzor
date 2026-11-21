package messages

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

// mountAttachments uses the normal auth chain for writes. Downloads hide
// missing sessions in the same way as unreadable channels.
func (s *Service) mountAttachments(r chi.Router) {
	if s.files == nil {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		r.With(auth.LimitPerUser(s.uploadLimiter), upload.Limit(auth.UserKey)).
			Post("/channels/{channel_id}/attachments", httpx.Wrap(s.handleUploadAttachment))
		r.With(auth.LimitPerUser(s.writeLimiter)).
			Delete("/channels/{channel_id}/attachments/{attachment_id}", httpx.Wrap(s.handleDiscardAttachment))
	})
	r.With(httpx.NoStore).
		Get("/attachments/{channel_id}/{attachment_id}/{filename}", httpx.Wrap(s.handleReadAttachment))
}

// handleUploadAttachment authorizes before reading any of the request body.
func (s *Service) handleUploadAttachment(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	user := auth.CurrentUser(r).ID
	_, guildID, err := s.access(ctx, user, ch, permissions.ViewChannel|permissions.SendMessages|permissions.AttachFiles)
	if err != nil {
		return fail(err)
	}
	if err = s.requireText(ctx, ch); err != nil {
		return fail(err)
	}
	if guildID == 0 {
		if err = s.checkPrivateWriter(ctx, ch, user); err != nil {
			return fail(err)
		}
	}
	if err = s.checkAttachmentSpace(); err != nil {
		return err
	}
	upload.ExtendDeadlines(w)
	id := s.ids.Next()
	ref := files.AttachmentReference(ch, id)
	var (
		name  string
		info  files.AttachmentInfo
		saved bool
	)
	err = upload.StreamFile(w, r, files.MaxAttachmentBytes, func(filename string, src io.Reader) error {
		name = attachmentFilename(filename)
		var saveErr error
		info, saveErr = s.files.SaveAttachment(ctx, ref, src)
		saved = saveErr == nil
		return saveErr
	})
	if err != nil {
		if saved {
			s.removeUncommitted(ref)
		}
		if errors.Is(err, files.ErrAttachmentTooLarge) {
			return upload.TooLarge(files.MaxAttachmentBytes)
		}
		return fmt.Errorf("messages: upload attachment: %w", err)
	}
	params := store.CreateAttachmentParams{ID: int64(id), ChannelID: int64(ch),
		UploaderID: dbID(user), Filename: name, ContentType: info.ContentType, Size: info.Size,
		Width: dbDimension(info.Width), Height: dbDimension(info.Height), Digest: info.Digest, CreatedAt: s.now()}
	var row store.MessageAttachment
	insertAttempted := false
	record := func(q store.DBTX) error {
		if quotaErr := s.checkAttachmentQuota(ctx, q, user, params.Size); quotaErr != nil {
			return quotaErr
		}
		var recordErr error
		insertAttempted = true
		row, recordErr = s.q.CreateAttachment(ctx, q, params)
		return recordErr
	}
	if guildID == 0 {
		err = s.withChannelWrite(ctx, 0, ch, func(tx pgx.Tx) error {
			if accessErr := s.checkPrivateWriterIn(ctx, tx, ch, user); accessErr != nil {
				return accessErr
			}
			return record(tx)
		})
	} else if s.attachmentQuota > 0 {
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return record(tx) })
	} else {
		err = record(s.pool)
	}
	if err != nil {
		if errors.Is(err, errAttachmentQuota) {
			s.removeUncommitted(ref)
			return &httpx.Problem{Status: http.StatusRequestEntityTooLarge, Code: "attachment_quota_exceeded",
				Detail: "You have reached the storage limit for your files. Delete old attachments or ask an administrator."}
		}
		if !insertAttempted {
			s.removeUncommitted(ref)
			return fail(err)
		}
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
			s.removeUncommitted(ref)
			return httpx.NotFound()
		}
		// A lost database response leaves the commit outcome unknown. Reconcile
		// can recover an orphan after its grace window.
		return fmt.Errorf("messages: record attachment: %w", err)
	}
	httpx.JSON(w, http.StatusCreated, attachment(row))
	return nil
}

// removeUncommitted removes a file whose metadata was never committed.
func (s *Service) removeUncommitted(ref files.Reference) {
	if err := s.files.RemoveUncommittedAttachment(ref); err != nil {
		s.logger.Warn("remove uncommitted attachment", "err", err)
	}
}

// attachmentNotFound is the response for missing or inaccessible attachments.
func attachmentNotFound() *httpx.Problem {
	return &httpx.Problem{Status: http.StatusNotFound, Code: "attachment_not_found"}
}

// attachmentPath treats malformed IDs the same way as missing attachments.
func attachmentPath(r *http.Request) (snowflake.ID, snowflake.ID, error) {
	ch, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return 0, 0, attachmentNotFound()
	}
	id, err := httpx.PathID(r, "attachment_id")
	if err != nil {
		return 0, 0, attachmentNotFound()
	}
	return ch, id, nil
}

// handleDiscardAttachment deletes only the caller's pending metadata.
func (s *Service) handleDiscardAttachment(w http.ResponseWriter, r *http.Request) error {
	ch, id, err := attachmentPath(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	n, err := s.q.DiscardPendingAttachment(ctx, s.pool, store.DiscardPendingAttachmentParams{
		ID: int64(id), ChannelID: int64(ch), UploaderID: dbID(auth.CurrentUser(r).ID), Cutoff: s.now().Add(-PendingTTL),
	})
	if err != nil {
		return fmt.Errorf("messages: discard attachment: %w", err)
	}
	if n == 0 {
		return attachmentNotFound()
	}
	s.wakeAttachmentTrash()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleReadAttachment checks history access before opening the original.
func (s *Service) handleReadAttachment(w http.ResponseWriter, r *http.Request) error {
	ch, id, err := attachmentPath(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	user := auth.CurrentUser(r).ID
	if user == 0 {
		return attachmentNotFound()
	}
	if _, _, err = s.access(ctx, user, ch, permissions.ViewChannel|permissions.ReadMessageHistory); err != nil {
		if errors.Is(err, ErrMissingPerm) || errors.Is(err, channels.ErrNotFound) {
			return attachmentNotFound()
		}
		return fail(err)
	}
	row, err := s.q.AttachmentForReader(ctx, s.pool, store.AttachmentForReaderParams{
		ID: int64(id), ChannelID: int64(ch), ReaderID: dbID(user), Cutoff: s.now().Add(-PendingTTL),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return attachmentNotFound()
	}
	if err != nil {
		return fmt.Errorf("messages: load attachment: %w", err)
	}
	f, err := s.files.OpenAttachment(files.AttachmentReference(ch, id))
	if errors.Is(err, os.ErrNotExist) {
		return attachmentNotFound()
	}
	if err != nil {
		return fmt.Errorf("messages: open attachment: %w", err)
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("messages: stat attachment: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return attachmentNotFound()
	}
	serveAttachment(w, r, row, f)
	return nil
}

// serveAttachment provides conditional and range reads of immutable content.
func serveAttachment(w http.ResponseWriter, r *http.Request, row store.MessageAttachment, f *os.File) {
	disposition := "attachment"
	switch row.ContentType {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "video/mp4", "video/webm",
		"audio/mpeg", "audio/ogg", "audio/wave":
		disposition = "inline"
	}
	name := strings.ReplaceAll(url.QueryEscape(row.Filename), "+", "%20")
	h := w.Header()
	h.Set("Content-Type", row.ContentType)
	h.Set("Content-Disposition", disposition+"; filename*=UTF-8''"+name)
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-cache")
	h.Add("Vary", "Cookie")
	h.Set("ETag", `"`+row.Digest+`"`)
	http.ServeContent(w, r, row.Filename, row.CreatedAt, f)
}
