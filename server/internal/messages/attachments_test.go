package messages_test

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

type attachmentBody struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
	URL         string `json:"url"`
	Pending     bool   `json:"pending"`
}

func uploadAttachment(t *testing.T, c *testkit.Client, channel, name string, contents []byte) *testkit.Response {
	t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	w, err := m.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := w.Write(contents); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return c.Do(http.MethodPost, "/api/v1/channels/"+channel+"/attachments", body.Bytes(),
		testkit.WithHeader("Content-Type", m.FormDataContentType()))
}

func pendingFile(t *testing.T, c *testkit.Client, channel, name string, content []byte) attachmentBody {
	t.Helper()
	resp := uploadAttachment(t, c, channel, name, content)
	expect(t, resp, http.StatusCreated)
	var a attachmentBody
	resp.JSON(&a)
	return a
}

func TestAttachmentImageAndRange(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 3, 5))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	a := pendingFile(t, c, ch.ID, "wrong.txt", source.Bytes())
	if !a.Pending || a.Width == nil || *a.Width != 3 || a.Height == nil || *a.Height != 5 ||
		a.ContentType != "image/png" || a.Size != source.Len() {
		t.Errorf("attachment = %+v, want pending original PNG 3x5", a)
	}
	r := c.Do(http.MethodGet, a.URL, nil, testkit.WithHeader("Range", "bytes=0-7"))
	expect(t, r, http.StatusPartialContent)
	if !bytes.Equal(r.Body, source.Bytes()[:8]) {
		t.Errorf("range = %x, want %x", r.Body, source.Bytes()[:8])
	}
	for header, want := range map[string]string{
		"Cache-Control":           "private, no-cache",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if r.Header.Get("ETag") == "" {
		t.Error("ETag is empty")
	}
}

func TestAttachmentHTMLDownloadsAsBinary(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	a := pendingFile(t, c, ch.ID, "test.png", []byte("<html>test</html>"))
	r := c.Do(http.MethodGet, strings.TrimSuffix(a.URL, "test.png")+"cosmetic", nil)
	expect(t, r, http.StatusOK)
	if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := r.Header.Get("Content-Disposition"); got != "attachment; filename*=UTF-8''test.png" {
		t.Errorf("Content-Disposition = %q, want encoded download name", got)
	}
}

func TestAttachmentPrivacyAndBinding(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	a := pendingFile(t, alice, ch.ID, "example.txt", []byte("example"))
	problemOf(t, bob.Do(http.MethodGet, a.URL, nil), http.StatusNotFound, "attachment_not_found")
	problemOf(t, bob.Do(http.MethodPost, messagesPath(ch.ID), map[string]any{"attachment_ids": []string{a.ID}}),
		http.StatusBadRequest, "attachment_not_found")
	body := map[string]any{"attachment_ids": []string{a.ID}, "nonce": "attach-once"}
	first := alice.Do(http.MethodPost, messagesPath(ch.ID), body)
	expect(t, first, http.StatusCreated)
	var sent struct {
		ID          string           `json:"id"`
		Content     string           `json:"content"`
		Attachments []attachmentBody `json:"attachments"`
	}
	first.JSON(&sent)
	if len(sent.Attachments) != 1 || sent.Attachments[0].ID != a.ID || sent.Attachments[0].Pending || sent.Content != "" {
		t.Fatalf("message = %+v, want one bound attachment and empty content", sent)
	}
	retry := alice.Do(http.MethodPost, messagesPath(ch.ID), body)
	expect(t, retry, http.StatusCreated)
	if !bytes.Equal(first.Body, retry.Body) {
		t.Errorf("nonce retry = %s, want %s", retry.Body, first.Body)
	}
	download := bob.Do(http.MethodGet, a.URL, nil)
	expect(t, download, http.StatusOK)
	etag := download.Header.Get("ETag")
	if etag == "" {
		t.Fatal("attachment ETag is empty")
	}
	conditional := testkit.WithHeader("If-None-Match", etag)
	expect(t, bob.Do(http.MethodGet, a.URL, nil, conditional), http.StatusNotModified)
	denyEveryone(t, alice, g, ch.ID, permissions.ReadMessageHistory)
	problemOf(t, bob.Do(http.MethodGet, a.URL, nil, conditional), http.StatusNotFound, "attachment_not_found")
	expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, sent.ID), nil), http.StatusNoContent)
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments: %v", err)
	}
	assertOriginalMoved(t, e, ch.ID, a.ID)
	problemOf(t, alice.Do(http.MethodGet, a.URL, nil), http.StatusNotFound, "attachment_not_found")
}

func TestDiscardPendingAttachmentIsOwnerOnly(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.user("owner")
	member, u := e.user("member")
	g, ch, _ := newGuild(t, owner, "club")
	e.join(g, u)
	a := pendingFile(t, owner, ch.ID, "unneeded.txt", []byte("discard this"))
	endpoint := "/api/v1/channels/" + ch.ID + "/attachments/" + a.ID
	problemOf(t, member.Do(http.MethodDelete, endpoint, nil), http.StatusNotFound, "attachment_not_found")
	problemOf(t, testkit.NewClient(t, e.h).Do(http.MethodGet, a.URL, nil), http.StatusNotFound, "attachment_not_found")
	expect(t, owner.Do(http.MethodDelete, endpoint, nil), http.StatusNoContent)
	problemOf(t, owner.Do(http.MethodDelete, endpoint, nil), http.StatusNotFound, "attachment_not_found")
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments: %v", err)
	}
	assertOriginalMoved(t, e, ch.ID, a.ID)
}
