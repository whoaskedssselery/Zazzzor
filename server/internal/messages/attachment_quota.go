package messages

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

var errAttachmentQuota = errors.New("messages: attachment quota exceeded")

// checkAttachmentSpace avoids streaming an upload when the files volume is full.
func (s *Service) checkAttachmentSpace() error {
	if s.minFreeBytes == 0 {
		return nil
	}
	free, err := s.freeBytes()
	if err != nil {
		return fmt.Errorf("messages: check attachment volume: %w", err)
	}
	if free < s.minFreeBytes {
		return &httpx.Problem{Status: http.StatusInsufficientStorage, Code: "storage_full",
			Detail: "The server is out of space for files. Tell an administrator."}
	}
	return nil
}

// checkAttachmentQuota serializes the uploader's quota read and subsequent
// insert in the caller's transaction. Pending rows count until deleted.
func (s *Service) checkAttachmentQuota(ctx context.Context, q store.DBTX, user snowflake.ID, size int64) error {
	if s.attachmentQuota == 0 {
		return nil
	}
	if err := s.q.LockAttachmentQuota(ctx, q, user.String()); err != nil {
		return fmt.Errorf("messages: lock attachment quota: %w", err)
	}
	used, err := s.q.AttachmentBytesByUploader(ctx, q, dbID(user))
	if err != nil {
		return fmt.Errorf("messages: read attachment quota: %w", err)
	}
	if size > s.attachmentQuota-used {
		return errAttachmentQuota
	}
	return nil
}
