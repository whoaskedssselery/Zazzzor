package messages_test

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestAttachmentUploadSize(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	data := make([]byte, files.MaxAttachmentBytes+1)
	a := pendingFile(t, c, ch.ID, "large.bin", data[:files.MaxAttachmentBytes])
	if a.Size != files.MaxAttachmentBytes {
		t.Errorf("attachment size = %d, want %d", a.Size, files.MaxAttachmentBytes)
	}
	problemOf(t, uploadAttachment(t, c, ch.ID, "large.bin", data), http.StatusRequestEntityTooLarge, "file_too_large")
}

func TestAttachmentUploadRate(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	for range 10 {
		pendingFile(t, c, ch.ID, "file.bin", nil)
	}
	r := uploadAttachment(t, c, ch.ID, "file.bin", nil)
	problemOf(t, r, http.StatusTooManyRequests, "rate_limited")
	if r.Header.Get("Retry-After") == "" {
		t.Error("Retry-After is missing")
	}
}

func TestAttachFilesRequiredAtUploadAndSend(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("owner")
	member, u := e.user("member")
	g, ch, _ := newGuild(t, owner, "club")
	e.join(g, u)
	a := pendingFile(t, member, ch.ID, "file.bin", nil)
	denyEveryone(t, owner, g, ch.ID, permissions.AttachFiles)
	problemOf(t, uploadAttachment(t, member, ch.ID, "file.bin", nil), http.StatusForbidden, "missing_permission")
	problemOf(t, member.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": []string{a.ID}}),
		http.StatusForbidden, "missing_permission")
}

func TestAttachmentIDsMustBePendingInTheChannel(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	g, ch, _ := newGuild(t, c, "club")
	other := createChannel(t, c, g, "other")
	a := pendingFile(t, c, ch.ID, "file.bin", nil)
	b := pendingFile(t, c, other.ID, "file.bin", nil)
	send := func(ids ...string) int {
		t.Helper()
		r := c.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": ids})
		return r.Status
	}
	for _, ids := range [][]string{{a.ID, b.ID}, {a.ID, a.ID}} {
		if got := send(ids...); got != http.StatusBadRequest {
			t.Errorf("send with %v = %d, want 400", ids, got)
		}
	}
	tooMany := slices.Repeat([]string{a.ID}, messages.MaxAttachments+1)
	r := c.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": tooMany})
	p := problemOf(t, r, http.StatusBadRequest, "too_many_attachments")
	if len(p.Errors) != 1 || p.Errors[0].Field != "attachment_ids" {
		t.Errorf("errors = %v, want attachment_ids", p.Errors)
	}
	if got := send(a.ID); got != http.StatusCreated {
		t.Errorf("send after rejected bind = %d, want 201", got)
	}
	if got := send(a.ID); got != http.StatusBadRequest {
		t.Errorf("send with bound ID = %d, want 400", got)
	}
	problemOf(t, c.Do(http.MethodPost, messagesPath(ch.ID), map[string]string{}), http.StatusBadRequest, "validation_failed")
}

func TestMessageAttachmentOrderInHistoryAndEvents(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	a := pendingFile(t, c, ch.ID, "a.bin", nil)
	b := pendingFile(t, c, ch.ID, "b.bin", nil)
	r := c.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": []string{b.ID, a.ID}})
	expect(t, r, http.StatusCreated)
	var sent messageBody
	r.JSON(&sent)
	expect(t, c.Do(http.MethodPatch, messagePath(ch.ID, sent.ID), map[string]string{"content": "caption"}), http.StatusOK)
	for _, typ := range []string{events.MessageCreate, events.MessageUpdate} {
		ev := e.pub.ofType(typ)
		if len(ev) != 1 {
			t.Fatalf("%s event count = %d, want 1", typ, len(ev))
		}
		msg, ok := ev[0].Payload.(messages.PublicMessage)
		if !ok || len(msg.Attachments) != 2 || msg.Attachments[0].ID.String() != b.ID || msg.Attachments[1].ID.String() != a.ID {
			t.Errorf("%s payload = %+v, want ordered attachments", typ, ev[0].Payload)
		}
	}
	r = c.Do(http.MethodGet, messagesPath(ch.ID), nil)
	expect(t, r, http.StatusOK)
	var page []struct{ Attachments []attachmentBody }
	r.JSON(&page)
	if len(page) != 1 || len(page[0].Attachments) != 2 || page[0].Attachments[0].ID != b.ID {
		t.Errorf("page = %+v, want one message with ordered attachments", page)
	}
}

func TestPendingExpiryAndChannelCascadeTrashOriginal(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	g, _, _ := newGuild(t, c, "club")
	ch := createChannel(t, c, g, "files")
	expired := pendingFile(t, c, ch.ID, "expired.bin", []byte("expired"))
	stays := pendingFile(t, c, ch.ID, "fresh.bin", []byte("fresh"))
	_, err := e.pool.Exec(t.Context(), "UPDATE message_attachments SET created_at = $1 WHERE id = $2",
		time.Now().Add(-2*messages.PendingTTL), int64(parseID(t, expired.ID)))
	if err != nil {
		t.Fatalf("age pending attachment: %v", err)
	}
	problemOf(t, c.Do(http.MethodGet, expired.URL, nil), http.StatusNotFound, "attachment_not_found")
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments: %v", err)
	}
	assertOriginalMoved(t, e, ch.ID, expired.ID)
	r := c.Do(http.MethodGet, stays.URL, nil)
	expect(t, r, http.StatusOK)
	if !bytes.Equal(r.Body, []byte("fresh")) {
		t.Errorf("fresh bytes = %q, want fresh", r.Body)
	}
	expect(t, c.Do(http.MethodDelete, "/api/v1/channels/"+ch.ID, nil), http.StatusNoContent)
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments after channel deletion: %v", err)
	}
	assertOriginalMoved(t, e, ch.ID, stays.ID)
}

func assertOriginalMoved(t *testing.T, e *env, channel, attachment string) {
	t.Helper()
	ref := files.AttachmentReference(parseID(t, channel), parseID(t, attachment))
	f, err := e.disk.OpenAttachment(ref)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("open deleted original = %v, want missing", err)
	}
}
