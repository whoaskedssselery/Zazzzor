package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
)

// Restore check: `zazzzor restore-check seed <base_url>` creates data
// through the API (a user with an avatar, a server whose icon is replaced
// once, so trash has an entry, and a message) and prints what it created as
// JSON on stdout. `zazzzor restore-check verify <base_url>` reads that JSON
// on stdin and checks, after a backup was restored, that the user can sign
// in, the message is there, and the avatar, icon, attachment and emoji
// download with the same bytes. deploy/scripts/restore-test.sh runs both inside the
// api container, like smoke; the data is left in place, so it is meant for
// a throwaway stack. The JSON holds the test user's password.

const restoreCheckUsage = "usage: zazzzor restore-check seed <base_url> > state.json; " +
	"zazzzor restore-check verify <base_url> < state.json"

// restoreState is what seed created and verify expects.
type restoreState struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	GuildID       string `json:"guild_id"`
	ChannelID     string `json:"channel_id"`
	MessageID     string `json:"message_id"`
	AvatarURL     string `json:"avatar_url"`
	IconURL       string `json:"icon_url"`
	AvatarSHA     string `json:"avatar_sha256"`
	IconSHA       string `json:"icon_sha256"`
	AttachmentURL string `json:"attachment_url,omitempty"`
	AttachmentSHA string `json:"attachment_sha256,omitempty"`
	EmojiURL      string `json:"emoji_url,omitempty"`
	EmojiSHA      string `json:"emoji_sha256,omitempty"`
}

// restoreCheckCommand parses `restore-check seed|verify <base_url>`; the
// Origin comes from ZAZZZOR_ALLOWED_ORIGINS, as for smoke.
func restoreCheckCommand(args []string, env func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 2 || (args[0] != "seed" && args[0] != "verify") {
		_, _ = fmt.Fprintln(stderr, restoreCheckUsage)
		return 2
	}
	base := strings.TrimRight(args[1], "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		_, _ = fmt.Fprintf(stderr, "the address must start with http:// or https://\n%s\n", restoreCheckUsage)
		return 2
	}
	origin, _, _ := strings.Cut(env("ZAZZZOR_ALLOWED_ORIGINS"), ",")
	origin = strings.TrimSpace(origin)
	if origin == "" {
		_, _ = fmt.Fprintln(stderr, "ZAZZZOR_ALLOWED_ORIGINS is not set: the check takes its Origin header from it")
		return 2
	}
	return runRestoreCheck(context.Background(), args[0], base, origin, stdin, stdout, stderr)
}

// runRestoreCheck runs seed or verify against base. Steps are printed one
// per line: to stderr for seed, whose stdout is the state, and to stdout
// for verify, which ends with OK. The first failed step is printed to
// stderr and gives 1; a state that cannot be read gives 2.
func runRestoreCheck(ctx context.Context, mode, base, origin string, stdin io.Reader, stdout, stderr io.Writer) int {
	s := &smokeSession{
		base:   base,
		origin: origin,
		http:   &http.Client{Timeout: smokeRequestTimeout},
		out:    stdout,
	}
	if mode == "seed" {
		s.out = stderr
		st, err := s.seed(ctx)
		if err == nil {
			err = json.NewEncoder(stdout).Encode(st) //nolint:gosec // G117: the state must carry the throwaway test user's password to sign in after the restore.
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err)
			return 1
		}
		return 0
	}
	var st restoreState
	if err := json.NewDecoder(stdin).Decode(&st); err != nil || st.Username == "" ||
		(st.AttachmentURL == "") != (st.AttachmentSHA == "") || (st.EmojiURL == "") != (st.EmojiSHA == "") {
		_, _ = fmt.Fprintf(stderr, "cannot read the state from restore-check seed on stdin: %v\n", err)
		return 2
	}
	if err := s.verify(ctx, st); err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "OK")
	return 0
}

// seed creates the data that verify checks after a restore.
func (s *smokeSession) seed(ctx context.Context) (restoreState, error) {
	st := restoreState{Username: "restore_" + randomHex(4), Password: randomHex(16)}
	if err := s.step("sign-up", s.register(ctx, st.Username, st.Password)); err != nil {
		return st, err
	}
	var user struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := s.step("avatar", s.upload(ctx, "/api/v1/users/@me/avatar", solidPNG(80), &user)); err != nil {
		return st, err
	}
	st.AvatarURL = user.AvatarURL
	var guild struct {
		ID      string
		IconURL string `json:"icon_url"`
	}
	if err := s.step("create server", s.do(ctx, http.MethodPost, "/api/v1/guilds",
		map[string]string{"name": "restore"}, http.StatusCreated, &guild)); err != nil {
		return st, err
	}
	st.GuildID = guild.ID
	if err := s.step("icon", s.upload(ctx, "/api/v1/guilds/"+guild.ID+"/icon", solidPNG(80), &guild)); err != nil {
		return st, err
	}
	iconURL, err := s.replaceIcon(ctx, guild.ID, guild.IconURL)
	if err = s.step("replace icon", err); err != nil {
		return st, err
	}
	st.IconURL = iconURL
	channelID, err := s.textChannel(ctx, guild.ID)
	if err = s.step("channel list", err); err != nil {
		return st, err
	}
	st.ChannelID = channelID
	var attachment struct{ ID, URL string }
	if err = s.step("attachment", s.uploadFile(ctx, http.MethodPost, "/api/v1/channels/"+channelID+"/attachments",
		"restore.txt", []byte("restore attachment\n"), nil, http.StatusCreated, &attachment)); err != nil {
		return st, err
	}
	st.AttachmentURL = attachment.URL
	var msg struct{ ID string }
	if err = s.step("send message", s.do(ctx, http.MethodPost, "/api/v1/channels/"+channelID+"/messages",
		map[string]any{"content": "restore", "attachment_ids": []string{attachment.ID}}, http.StatusCreated, &msg)); err != nil {
		return st, err
	}
	st.MessageID = msg.ID
	var emoji struct{ URL string }
	if err = s.step("emoji", s.uploadFile(ctx, http.MethodPost, "/api/v1/guilds/"+guild.ID+"/emojis",
		"emoji.png", solidPNG(110), map[string]string{"name": "restore"}, http.StatusCreated, &emoji)); err != nil {
		return st, err
	}
	st.EmojiURL = emoji.URL
	st.AttachmentSHA, err = s.digest(ctx, st.AttachmentURL)
	if err = s.step("download attachment", err); err != nil {
		return st, err
	}
	st.EmojiSHA, err = s.digest(ctx, st.EmojiURL)
	if err = s.step("download emoji", err); err != nil {
		return st, err
	}
	st.AvatarSHA, err = s.digest(ctx, st.AvatarURL)
	if err = s.step("download avatar", err); err != nil {
		return st, err
	}
	st.IconSHA, err = s.digest(ctx, st.IconURL)
	if err = s.step("download icon", err); err != nil {
		return st, err
	}
	return st, nil
}

// replaceIcon uploads a different icon for guildID over the one at old,
// checks that old is no longer served (it went to trash) and returns the
// new icon's URL.
func (s *smokeSession) replaceIcon(ctx context.Context, guildID, old string) (string, error) {
	var guild struct {
		IconURL string `json:"icon_url"`
	}
	if err := s.upload(ctx, "/api/v1/guilds/"+guildID+"/icon", solidPNG(180), &guild); err != nil {
		return "", err
	}
	if guild.IconURL == old {
		return "", errors.New("the icon address did not change")
	}
	if _, err := s.fetch(ctx, old); err == nil {
		return "", errors.New("the previous icon is still served")
	}
	return guild.IconURL, nil
}

// verify checks a restored API against what seed created.
func (s *smokeSession) verify(ctx context.Context, st restoreState) error {
	var user struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := s.step("sign-in", s.signIn(ctx, "/api/v1/auth/login", st.Username, st.Password, http.StatusOK, &user)); err != nil {
		return err
	}
	defer func() { _ = s.do(ctx, http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil) }()
	if user.AvatarURL != st.AvatarURL {
		return s.step("avatar", fmt.Errorf("avatar address %q, expected %q", user.AvatarURL, st.AvatarURL))
	}
	var guild struct {
		IconURL string `json:"icon_url"`
	}
	err := s.do(ctx, http.MethodGet, "/api/v1/guilds/"+st.GuildID, nil, http.StatusOK, &guild)
	if err == nil && guild.IconURL != st.IconURL {
		err = fmt.Errorf("icon address %q, expected %q", guild.IconURL, st.IconURL)
	}
	if err = s.step("server", err); err != nil {
		return err
	}
	if err = s.step("message", s.findMessage(ctx, st.ChannelID, st.MessageID, st.AttachmentURL)); err != nil {
		return err
	}
	if err = s.step("avatar", s.sameBytes(ctx, st.AvatarURL, st.AvatarSHA)); err != nil {
		return err
	}
	if err = s.step("icon", s.sameBytes(ctx, st.IconURL, st.IconSHA)); err != nil {
		return err
	}
	for _, file := range []struct{ label, url, sha string }{
		{"attachment", st.AttachmentURL, st.AttachmentSHA},
		{"emoji", st.EmojiURL, st.EmojiSHA},
	} {
		// Older seed states predate these files.
		if file.url != "" {
			if err = s.step(file.label, s.sameBytes(ctx, file.url, file.sha)); err != nil {
				return err
			}
		}
	}
	return nil
}

// digest downloads path and returns the hex SHA-256 of its body.
func (s *smokeSession) digest(ctx context.Context, path string) (string, error) {
	data, err := s.fetch(ctx, path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// sameBytes checks that path downloads with the SHA-256 want.
func (s *smokeSession) sameBytes(ctx context.Context, path, want string) error {
	got, err := s.digest(ctx, path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s serves other bytes than before the backup", path)
	}
	return nil
}

// solidPNG is an 8×8 PNG of one colour; shade makes images differ.
func solidPNG(shade uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: shade, G: 40, B: 120, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img) // writing to a bytes.Buffer cannot fail
	return b.Bytes()
}

// schemaVersionCommand prints the schema version this binary migrates a
// database to. A restore refuses a backup whose schema is newer, before it
// touches anything. It needs no configuration.
func schemaVersionCommand(stdout, stderr io.Writer) int {
	v, err := db.LatestMigration()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "schema-version: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, v)
	return 0
}
