package users_test

import (
	"bytes"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestGetMe(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	problemOf(t, c.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	u := register(t, c, "alice", "password123")
	resp := c.Do(http.MethodGet, "/api/v1/users/@me", nil)
	if resp.Status != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("GET @me = %d, Cache-Control %q", resp.Status, resp.Header.Get("Cache-Control"))
	}
	var got userBody
	resp.JSON(&got)
	if got != u {
		t.Fatalf("@me = %+v, want %+v", got, u)
	}
}

func TestUpdateDisplayName(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	u := register(t, c, "alice", "password123")
	id := userID(t, u)

	patch := func(body any) *testkit.Response {
		return c.Do(http.MethodPatch, "/api/v1/users/@me", body)
	}
	for _, dn := range []string{"Алиса", strings.Repeat("я", 32), "a"} {
		resp := patch(map[string]string{"display_name": dn})
		if resp.Status != http.StatusOK {
			t.Fatalf("display_name %q: status %d: %s", dn, resp.Status, resp.Body)
		}
		var got userBody
		resp.JSON(&got)
		if got.DisplayName != dn || got.ID != u.ID {
			t.Fatalf("display_name %q: got %+v", dn, got)
		}
	}
	if got := me(t, c).DisplayName; got != "a" {
		t.Fatalf("stored display_name = %q", got)
	}

	for _, body := range []any{
		map[string]string{"display_name": ""},
		map[string]string{"display_name": "   "},
		map[string]string{"display_name": strings.Repeat("я", 33)},
		map[string]any{"display_name": nil},
		map[string]any{"display_name": 42},
	} {
		p := problemOf(t, patch(body), http.StatusBadRequest, "validation_failed")
		if len(p.Errors) != 1 || p.Errors[0].Field != "display_name" {
			t.Fatalf("%v: errors = %+v", body, p.Errors)
		}
	}
	problemOf(t, patch(map[string]string{"username": "x"}), http.StatusBadRequest, "invalid_body")

	// An empty patch changes nothing and announces nothing.
	before := len(e.pub.peerUpdates())
	if resp := patch(map[string]string{}); resp.Status != http.StatusOK {
		t.Fatalf("empty patch = %d", resp.Status)
	}
	// Setting the same name again is not a change either.
	if resp := patch(map[string]string{"display_name": "a"}); resp.Status != http.StatusOK {
		t.Fatalf("same name = %d", resp.Status)
	}
	evs := e.pub.peerUpdates()
	if before != 3 || len(evs) != 3 {
		t.Fatalf("USER_UPDATE events = %d then %d, want 3 and 3", before, len(evs))
	}
	last := evs[2]
	if last.Audience.Kind != events.AudiencePeers || len(last.Audience.UserIDs) != 1 || last.Audience.UserIDs[0] != id {
		t.Fatalf("audience = %+v, want peers of %v", last.Audience, id)
	}
	if p, ok := last.Payload.(auth.PublicUser); !ok || p.DisplayName != "a" || p.ID != id {
		t.Fatalf("payload = %#v", last.Payload)
	}

	problemOf(t, c.Do(http.MethodPatch, "/api/v1/users/@me", map[string]string{"display_name": "b"}, testkit.WithoutOrigin()),
		http.StatusForbidden, "origin_not_allowed")
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	c1, c2 := e.client(), e.client()
	register(t, c1, "alice", "password123")
	login(t, c2, "alice", "password123")

	change := func(c *testkit.Client, current, next string) *testkit.Response {
		return c.Do(http.MethodPost, "/api/v1/users/@me/password",
			map[string]string{"current_password": current, "new_password": next})
	}
	p := problemOf(t, change(c1, "wrong-password", "password456"), http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "current_password" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	p = problemOf(t, change(c1, "password123", "short"), http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "new_password" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	// Nothing changed yet: both sessions work.
	me(t, c2)

	if resp := change(c1, "password123", "password456"); resp.Status != http.StatusNoContent {
		t.Fatalf("change = %d: %s", resp.Status, resp.Body)
	}
	me(t, c1)
	problemOf(t, c2.Do(http.MethodGet, "/api/v1/users/@me", nil), http.StatusUnauthorized, "unauthenticated")
	if revoked := e.pub.ofType(events.SessionRevoked); len(revoked) != 1 {
		t.Fatalf("SESSION_REVOKED events = %d, want 1", len(revoked))
	}

	c3 := e.client()
	problemOf(t, c3.Do(http.MethodPost, "/api/v1/auth/login", creds("alice", "password123")),
		http.StatusUnauthorized, "invalid_credentials")
	login(t, c3, "alice", "password456")

	problemOf(t, e.client().Do(http.MethodPost, "/api/v1/users/@me/password",
		map[string]string{"current_password": "password456", "new_password": "password789"}),
		http.StatusUnauthorized, "unauthenticated")
}

func TestChangePasswordRateLimited(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "alice", "password123")
	body := map[string]string{"current_password": "wrong-password", "new_password": "password456"}
	for i := range 5 {
		if resp := c.Do(http.MethodPost, "/api/v1/users/@me/password", body); resp.Status != http.StatusBadRequest {
			t.Fatalf("attempt %d = %d: %s", i+1, resp.Status, resp.Body)
		}
	}
	body["current_password"] = "password123"
	resp := c.Do(http.MethodPost, "/api/v1/users/@me/password", body)
	problemOf(t, resp, http.StatusTooManyRequests, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// The limit is per user: another account is not affected.
	other := e.client()
	register(t, other, "bob", "password123")
	if resp := other.Do(http.MethodPost, "/api/v1/users/@me/password", body); resp.Status != http.StatusNoContent {
		t.Fatalf("other user = %d: %s", resp.Status, resp.Body)
	}
}

func avatarPath(e *env, u userBody) string {
	// avatar_url is /api/v1/files/avatars/<id>/<hash>.png
	rest := strings.TrimPrefix(*u.AvatarURL, "/api/v1/files/")
	return filepath.Join(e.dir, filepath.FromSlash(rest))
}

func TestAvatarUploadNormalizes(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	u := register(t, c, "alice", "password123")
	if u.AvatarURL != nil {
		t.Fatalf("new account has avatar %q", *u.AvatarURL)
	}
	src := photo(t, 800, 600, color.RGBA{R: 200, G: 30, B: 30, A: 255})

	// The name and part type are ignored: the content decides.
	resp := uploadAvatar(t, c, "me.gif", "application/octet-stream", src)
	if resp.Status != http.StatusOK {
		t.Fatalf("upload = %d: %s", resp.Status, resp.Body)
	}
	var got userBody
	resp.JSON(&got)
	prefix := "/api/v1/files/avatars/" + u.ID + "/"
	if got.AvatarURL == nil || !strings.HasPrefix(*got.AvatarURL, prefix) {
		t.Fatalf("avatar_url = %v, want under %s", got.AvatarURL, prefix)
	}
	if m := me(t, c); m.AvatarURL == nil || *m.AvatarURL != *got.AvatarURL {
		t.Fatalf("@me avatar_url = %v", m.AvatarURL)
	}

	file := c.Do(http.MethodGet, *got.AvatarURL, nil)
	if file.Status != http.StatusOK {
		t.Fatalf("GET %s = %d", *got.AvatarURL, file.Status)
	}
	img, err := png.Decode(bytes.NewReader(file.Body))
	if err != nil {
		t.Fatalf("served avatar is not PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("served avatar is %v, want 256x256", b)
	}

	if evs := e.pub.peerUpdates(); len(evs) != 1 || evs[0].Audience.Kind != events.AudiencePeers {
		t.Fatalf("USER_UPDATE events = %+v", evs)
	}

	// The same file again gives the same URL, keeps the file and announces
	// nothing.
	resp = uploadAvatar(t, c, "again.jpg", "image/jpeg", src)
	var again userBody
	resp.JSON(&again)
	if resp.Status != http.StatusOK || again.AvatarURL == nil || *again.AvatarURL != *got.AvatarURL {
		t.Fatalf("same upload = %d, %v; want %s", resp.Status, again.AvatarURL, *got.AvatarURL)
	}
	if c.Do(http.MethodGet, *got.AvatarURL, nil).Status != http.StatusOK {
		t.Fatal("re-uploading the same image removed it")
	}
	if n := len(e.pub.peerUpdates()); n != 1 {
		t.Fatalf("USER_UPDATE events after same upload = %d, want 1", n)
	}

	// A different image replaces the old file.
	resp = uploadAvatar(t, c, "blue.jpg", "image/jpeg", photo(t, 300, 300, color.RGBA{B: 200, A: 255}))
	var next userBody
	resp.JSON(&next)
	if resp.Status != http.StatusOK || next.AvatarURL == nil || *next.AvatarURL == *got.AvatarURL {
		t.Fatalf("new upload = %d, %v", resp.Status, next.AvatarURL)
	}
	if _, err := os.Stat(avatarPath(e, got)); !os.IsNotExist(err) {
		t.Fatalf("old avatar file still exists: %v", err)
	}
	if s := c.Do(http.MethodGet, *got.AvatarURL, nil).Status; s != http.StatusNotFound {
		t.Fatalf("old avatar URL = %d, want 404", s)
	}

	// Delete clears it and removes the file.
	resp = c.Do(http.MethodDelete, "/api/v1/users/@me/avatar", nil)
	var cleared userBody
	resp.JSON(&cleared)
	if resp.Status != http.StatusOK || cleared.AvatarURL != nil {
		t.Fatalf("delete = %d, %v", resp.Status, cleared.AvatarURL)
	}
	if _, err := os.Stat(avatarPath(e, next)); !os.IsNotExist(err) {
		t.Fatalf("deleted avatar file still exists: %v", err)
	}
	if n := len(e.pub.peerUpdates()); n != 3 {
		t.Fatalf("USER_UPDATE events = %d, want 3", n)
	}
	if resp := c.Do(http.MethodDelete, "/api/v1/users/@me/avatar", nil); resp.Status != http.StatusOK {
		t.Fatalf("second delete = %d", resp.Status)
	}
}

func TestAvatarRejectsNonImage(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "alice", "password123")
	problemOf(t, uploadAvatar(t, c, "x.png", "image/png", []byte("just some text, honestly\n")),
		http.StatusBadRequest, "unsupported_image")

	// Not multipart at all, or without a file part.
	problemOf(t, c.Do(http.MethodPut, "/api/v1/users/@me/avatar", map[string]string{"file": "x"}),
		http.StatusUnsupportedMediaType, "unsupported_media_type")
	body, ct := multipartBody(t, "picture", "x.jpg", "image/jpeg", photo(t, 10, 10, color.White))
	p := problemOf(t, c.Do(http.MethodPut, "/api/v1/users/@me/avatar", body, testkit.WithHeader("Content-Type", ct)),
		http.StatusBadRequest, "validation_failed")
	if len(p.Errors) != 1 || p.Errors[0].Field != "file" {
		t.Fatalf("errors = %+v", p.Errors)
	}
	problemOf(t, c.Do(http.MethodPut, "/api/v1/users/@me/avatar", "--zzz\r\nnot a header line\r\n\r\nx\r\n--zzz--\r\n",
		testkit.WithHeader("Content-Type", "multipart/form-data; boundary=zzz")),
		http.StatusBadRequest, "invalid_body")
	if m := me(t, c); m.AvatarURL != nil {
		t.Fatalf("a rejected upload set avatar %q", *m.AvatarURL)
	}
	if n := len(e.pub.peerUpdates()); n != 0 {
		t.Fatalf("USER_UPDATE events = %d, want 0", n)
	}
}

func TestAvatarTooLarge(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	register(t, c, "alice", "password123")
	// A real JPEG followed by padding: 5 MB in all.
	content := append(photo(t, 100, 100, color.White), make([]byte, 5<<20)...)
	problemOf(t, uploadAvatar(t, c, "big.jpg", "image/jpeg", content), http.StatusRequestEntityTooLarge, "file_too_large")

	// Just over 4 MiB inside a body that fits the multipart allowance.
	content = append(photo(t, 100, 100, color.White), make([]byte, 4<<20)...)
	problemOf(t, uploadAvatar(t, c, "big.jpg", "image/jpeg", content), http.StatusRequestEntityTooLarge, "file_too_large")
	if m := me(t, c); m.AvatarURL != nil {
		t.Fatalf("a rejected upload set avatar %q", *m.AvatarURL)
	}
}
