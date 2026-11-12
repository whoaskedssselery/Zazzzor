package messages_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

func TestAttachmentQuotaCountsPendingAcrossChannels(t *testing.T) {
	e := newEnv(t, func(o *messages.Options) { o.AttachmentQuotaBytes = 4 })
	c, _ := e.user("alice")
	_, first, _ := newGuild(t, c, "first")
	_, second, _ := newGuild(t, c, "second")
	pendingFile(t, c, first.ID, "first.txt", []byte("123"))
	pendingFile(t, c, second.ID, "last.txt", []byte("4"))
	problemOf(t, uploadAttachment(t, c, second.ID, "over.txt", []byte("5")),
		http.StatusRequestEntityTooLarge, "attachment_quota_exceeded")
}

func TestAttachmentQuotaReleasesOnMessageDeletion(t *testing.T) {
	e := newEnv(t, func(o *messages.Options) { o.AttachmentQuotaBytes = 4 })
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "guild")
	file := pendingFile(t, c, ch.ID, "full.txt", []byte("1234"))
	res := c.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": []string{file.ID}})
	expect(t, res, http.StatusCreated)
	var msg struct{ ID string }
	res.JSON(&msg)
	problemOf(t, uploadAttachment(t, c, ch.ID, "over.txt", []byte("5")),
		http.StatusRequestEntityTooLarge, "attachment_quota_exceeded")
	expect(t, c.Do(http.MethodDelete, messagePath(ch.ID, msg.ID), nil), http.StatusNoContent)
	pendingFile(t, c, ch.ID, "again.txt", []byte("1234"))
}

func TestAttachmentStorageFloor(t *testing.T) {
	for _, tc := range []struct {
		name string
		free uint64
		want int
	}{
		{"below", 9, http.StatusInsufficientStorage},
		{"exact", 10, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(o *messages.Options) {
				o.MinFreeBytes = 10
				o.FreeBytes = func() (uint64, error) { return tc.free, nil }
			})
			c, _ := e.user("alice")
			_, ch, _ := newGuild(t, c, "guild")
			res := uploadAttachment(t, c, ch.ID, "data.txt", []byte("1"))
			if tc.want == http.StatusInsufficientStorage {
				problemOf(t, res, tc.want, "storage_full")
			} else {
				expect(t, res, tc.want)
			}
		})
	}
}
