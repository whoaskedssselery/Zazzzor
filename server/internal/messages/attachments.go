package messages

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	// MaxAttachments is the most files one message may bind.
	MaxAttachments = 10
	// PendingTTL is how long an unbound upload is available to its uploader.
	PendingTTL       = time.Hour
	maxFilenameBytes = 255
)

// Attachment is the wire form of an uploaded file.
type Attachment struct {
	ID          snowflake.ID `json:"id"`
	Filename    string       `json:"filename"`
	ContentType string       `json:"content_type"`
	Size        int64        `json:"size"`
	Width       *int32       `json:"width"`
	Height      *int32       `json:"height"`
	URL         string       `json:"url"`
	Pending     bool         `json:"pending"`
}

// attachment presents the same metadata before and after binding.
func attachment(row store.MessageAttachment) Attachment {
	a := Attachment{ID: snowflake.ID(row.ID), Filename: row.Filename, ContentType: row.ContentType,
		Size: row.Size, Pending: !row.MessageID.Valid,
		URL: fmt.Sprintf("/api/v1/attachments/%d/%d/%s", row.ChannelID, row.ID, url.PathEscape(row.Filename))}
	if row.Width.Valid {
		a.Width = &row.Width.Int32
	}
	if row.Height.Valid {
		a.Height = &row.Height.Int32
	}
	return a
}

// attachmentFilename strips path components and characters unsafe in a display name.
func attachmentFilename(raw string) string {
	name := path.Base(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if len(name) > maxFilenameBytes {
		end := maxFilenameBytes
		for !utf8.RuneStart(name[end]) {
			end--
		}
		name = strings.TrimSpace(name[:end])
	}
	switch name {
	case "", ".", "..", "/":
		return "file"
	default:
		return name
	}
}

// dbID marks a non-null ID for SQL parameters that reference a nullable column.
func dbID(id snowflake.ID) pgtype.Int8 { return pgtype.Int8{Int64: int64(id), Valid: true} }

// dbDimension preserves null for files without an image dimension.
func dbDimension(n *int32) pgtype.Int4 {
	if n == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *n, Valid: true}
}

// AttachmentReferences lists all file keys for backup reconciliation.
func (s *Service) AttachmentReferences(ctx context.Context) ([]files.Reference, error) {
	rows, err := s.q.AttachmentReferences(ctx, s.pool)
	if err != nil {
		return nil, fmt.Errorf("messages: list attachment references: %w", err)
	}
	refs := make([]files.Reference, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, files.AttachmentReference(snowflake.ID(row.ChannelID), snowflake.ID(row.ID)))
	}
	return refs, nil
}

// AttachmentReferenced checks a file key again before reconcile moves it.
func (s *Service) AttachmentReferenced(ctx context.Context, channel, id snowflake.ID) (bool, error) {
	ok, err := s.q.AttachmentReferenced(ctx, s.pool, store.AttachmentReferencedParams{ChannelID: int64(channel), ID: int64(id)})
	if err != nil {
		return false, fmt.Errorf("messages: check attachment reference: %w", err)
	}
	return ok, nil
}

// SweepAttachments expires pending metadata and retries queued file moves.
func (s *Service) SweepAttachments(ctx context.Context) error {
	var expireErr error
	if err := s.q.ExpirePendingAttachments(ctx, s.pool, s.now().Add(-PendingTTL)); err != nil {
		expireErr = fmt.Errorf("messages: expire pending attachments: %w", err)
	}
	return errors.Join(expireErr, s.moveAttachmentTrash(ctx))
}

// moveAttachmentTrash runs after commit so even a failed rename can be retried.
func (s *Service) moveAttachmentTrash(ctx context.Context) error {
	if s.files == nil {
		return nil
	}
	s.trashMu.Lock()
	defer s.trashMu.Unlock()
	rows, err := s.q.QueuedAttachmentTrash(ctx, s.pool)
	if err != nil {
		return fmt.Errorf("messages: list attachment trash: %w", err)
	}
	var failures []error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("messages: drain attachment trash: %w", err)
		}
		if err := s.files.DeleteAttachment(files.AttachmentReference(snowflake.ID(row.ChannelID), snowflake.ID(row.ID))); err != nil {
			failures = append(failures, fmt.Errorf("messages: trash attachment %d: %w", row.ID, err))
			continue
		}
		if err := s.q.ForgetAttachmentTrash(ctx, s.pool, row.ID); err != nil {
			failures = append(failures, fmt.Errorf("messages: acknowledge attachment trash %d: %w", row.ID, err))
		}
	}
	return errors.Join(failures...)
}

// tryAttachmentTrash attempts the file move after a committed deletion.
func (s *Service) tryAttachmentTrash(ctx context.Context) {
	if err := s.moveAttachmentTrash(ctx); err != nil {
		s.logger.Warn("move attachment trash", "err", err)
	}
}

// RunAttachmentSweep expires pending uploads and retries file moves until ctx ends.
func (s *Service) RunAttachmentSweep(ctx context.Context) {
	const interval = time.Minute
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.SweepAttachments(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("sweep attachments", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
