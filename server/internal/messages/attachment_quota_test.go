package messages_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

func TestAttachmentQuotaCountsPendingAcrossChannels(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestConcurrentUploadsCannotOverspendQuota(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(o *messages.Options) { o.AttachmentQuotaBytes = 4 })
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "guild")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err = blocker.Exec(ctx, "LOCK TABLE message_attachments IN SHARE MODE"); err != nil {
		t.Fatalf("block inserts: %v", err)
	}

	var statuses [2]int
	var uploads sync.WaitGroup
	for i := range statuses {
		uploads.Go(func() {
			statuses[i] = uploadAttachment(t, c, ch.ID, "race.txt", []byte("1234")).Status
		})
	}
	const pending = "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() AND wait_event_type = 'Lock' AND (query LIKE '%CreateAttachment :one%' OR query LIKE '%LockAttachmentQuota :exec%')"
	for {
		var waiting int
		if err = e.pool.QueryRow(ctx, pending).Scan(&waiting); err != nil {
			t.Fatalf("wait for uploads: %v", err)
		}
		if waiting >= len(statuses) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatalf("unblock inserts: %v", err)
	}
	uploads.Wait()
	if statuses[0]+statuses[1] != http.StatusCreated+http.StatusRequestEntityTooLarge ||
		(statuses[0] != http.StatusCreated && statuses[1] != http.StatusCreated) {
		t.Fatalf("concurrent upload statuses = %v, want 201 and 413", statuses)
	}
}
