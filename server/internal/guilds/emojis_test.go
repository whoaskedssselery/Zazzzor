package guilds_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func uploadEmoji(t *testing.T, c *testkit.Client, path, name string, raw []byte) *testkit.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	// Put the name second to exercise order-independent form parsing.
	p, err := mw.CreateFormFile("file", "emoji")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err = p.Write(raw); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err = mw.WriteField("name", name); err != nil {
		t.Fatalf("write name part: %v", err)
	}
	if err = mw.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}
	return c.Do(http.MethodPost, path, body.Bytes(), testkit.WithHeader("Content-Type", mw.FormDataContentType()))
}

func TestGuildEmojiPermissionsAndFile(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	stranger, _ := e.user("stranger")
	g := createGuild(t, alice, "guild")
	e.join(g.id(t), b.id(t))
	path := "/api/v1/guilds/" + g.ID + "/emojis"
	raw := pngImage(t, color.White)
	expect(t, uploadEmoji(t, bob, path, "face", raw), http.StatusForbidden)
	expect(t, uploadEmoji(t, alice, path, "x-", raw), http.StatusBadRequest)
	expect(t, uploadEmoji(t, alice, path, "large", make([]byte, 256<<10+1)), http.StatusRequestEntityTooLarge)
	res := uploadEmoji(t, alice, path, "Face", raw)
	expect(t, res, http.StatusCreated)
	var emoji guilds.Emoji
	res.JSON(&emoji)
	if emoji.Name != "Face" || emoji.GuildID != g.id(t) || emoji.Animated || emoji.CreatorID == nil {
		t.Fatalf("emoji = %+v, want static Face", emoji)
	}
	expect(t, uploadEmoji(t, alice, path, "fACE", raw), http.StatusBadRequest)
	expect(t, stranger.Do(http.MethodGet, path, nil), http.StatusNotFound)
	res = bob.Do(http.MethodGet, path, nil)
	expect(t, res, http.StatusOK)
	var list []guilds.Emoji
	res.JSON(&list)
	if len(list) != 1 || list[0].ID != emoji.ID {
		t.Fatalf("member list = %+v, want the emoji", list)
	}
	res = bob.Do(http.MethodGet, emoji.URL, nil)
	expect(t, res, http.StatusOK)
	if res.Header.Get("Content-Type") != "image/png" || res.Header.Get("Cache-Control") != "private, no-cache" ||
		res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("image headers = %v, want private PNG with nosniff", res.Header)
	}
	expect(t, stranger.Do(http.MethodGet, emoji.URL, nil), http.StatusNotFound)
	expect(t, testkit.NewClient(t, e.h).Do(http.MethodGet, emoji.URL, nil), http.StatusNotFound)
	edit := path + "/" + emoji.ID.String()
	expect(t, bob.Do(http.MethodPatch, edit, map[string]string{"name": "new"}), http.StatusForbidden)
	expect(t, bob.Do(http.MethodDelete, edit, nil), http.StatusForbidden)
	role := createRole(t, alice, g, "emoji manager", permissions.ManageExpressions)
	expect(t, setRoles(alice, g, b.ID, role.ID), http.StatusOK)
	expect(t, bob.Do(http.MethodPatch, edit, map[string]string{"name": "new"}), http.StatusOK)
	expect(t, bob.Do(http.MethodPatch, edit, map[string]string{"name": "new"}), http.StatusOK)
	expect(t, bob.Do(http.MethodDelete, edit, nil), http.StatusNoContent)
	expect(t, bob.Do(http.MethodGet, emoji.URL, nil), http.StatusNotFound)
	got := e.pub.ofType(events.GuildEmojisUpdate)
	if len(got) != 3 {
		t.Fatalf("emoji events = %d, want 3", len(got))
	}
	for _, evt := range got {
		if evt.Audience.Kind != events.AudienceGuild || evt.Audience.GuildID != g.id(t) {
			t.Errorf("emoji event audience = %+v, want guild", evt.Audience)
		}
	}
}

func TestGuildEmojiCapacityAndGuildDeletion(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	g := createGuild(t, alice, "guild")
	path := "/api/v1/guilds/" + g.ID + "/emojis"
	raw := pngImage(t, color.White)
	var last guilds.Emoji
	for i := range 50 {
		res := uploadEmoji(t, alice, path, fmt.Sprintf("emoji%d", i), raw)
		expect(t, res, http.StatusCreated)
		res.JSON(&last)
	}
	expect(t, uploadEmoji(t, alice, path, "full", raw), http.StatusBadRequest)
	var gifBytes bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
	if err := gif.EncodeAll(&gifBytes, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{2, 2}}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	res := uploadEmoji(t, alice, path, "motion", gifBytes.Bytes())
	expect(t, res, http.StatusCreated)
	var animated guilds.Emoji
	res.JSON(&animated)
	if !animated.Animated {
		t.Fatal("animated emoji reported static")
	}
	res = alice.Do(http.MethodGet, animated.URL, nil)
	expect(t, res, http.StatusOK)
	if res.Header.Get("Content-Type") != "image/gif" || !bytes.Equal(res.Body, gifBytes.Bytes()) {
		t.Error("served animation changed")
	}
	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID, nil), http.StatusNoContent)
	for _, id := range []guilds.Emoji{last, animated} {
		expect(t, alice.Do(http.MethodGet, id.URL, nil), http.StatusNotFound)
		f, err := e.disk.OpenAttachment(files.EmojiReference(id.ID))
		if f != nil {
			_ = f.Close()
		}
		if !os.IsNotExist(err) {
			t.Errorf("open deleted emoji %s = %v, want missing", id.ID, err)
		}
	}
}

func TestEmojiCommitRejectionLeavesNoLiveFile(t *testing.T) {
	e := newEnv(t)
	alice, owner := e.user("alice")
	g := createGuild(t, alice, "guild")
	_, err := e.pool.Exec(t.Context(), `
  CREATE FUNCTION reject_emoji_for_test() RETURNS trigger LANGUAGE plpgsql AS $$
  BEGIN RAISE EXCEPTION 'reject emoji at commit' USING ERRCODE = '23514'; END;
  $$;
  CREATE CONSTRAINT TRIGGER reject_emoji_for_test
  AFTER INSERT ON guild_emojis DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION reject_emoji_for_test();
 `)
	if err != nil {
		t.Fatalf("install deferred trigger: %v", err)
	}
	if _, err = e.svc.CreateEmoji(t.Context(), g.id(t), owner.id(t), "rejected", pngImage(t, color.White)); err == nil {
		t.Fatal("deferred constraint did not reject upload")
	}
	ref := files.EmojiReference(1)
	got, err := os.ReadDir(filepath.Join(e.dir, string(ref.Kind), ref.OwnerID.String()))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read emoji storage: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("live files = %v, want none after rollback", got)
	}
	if n := len(e.pub.ofType(events.GuildEmojisUpdate)); n != 0 {
		t.Errorf("updates = %d, want none", n)
	}
}
