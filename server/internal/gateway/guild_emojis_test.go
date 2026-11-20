package gateway_test

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestGuildEmojiSnapshotsAndAudience(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	outsider, o := e.user("outsider")
	g, _ := newGuild(t, alice, "guild")
	guildID, owner := parseID(t, g.ID), a.id(t)
	acl, _ := connect(t, alice)
	ocl, _ := connect(t, outsider)
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode emoji: %v", err)
	}
	emoji, err := e.m.Guilds.CreateEmoji(t.Context(), guildID, owner, "face", raw.Bytes())
	if err != nil {
		t.Fatalf("create emoji: %v", err)
	}
	var update guilds.EmojisUpdate
	decode(t, acl.waitFor(events.GuildEmojisUpdate).D, &update)
	if update.GuildID != guildID || len(update.Emojis) != 1 || update.Emojis[0].ID != emoji.ID {
		t.Fatalf("create update = %+v, want full emoji list", update)
	}
	bcl, _ := connect(t, bob)
	e.join(g, b)
	var created gateway.GuildState
	decode(t, bcl.waitFor(events.GuildCreate).D, &created)
	if len(created.Emojis) != 1 || created.Emojis[0].ID != emoji.ID {
		t.Fatalf("GUILD_CREATE emoji = %+v", created.Emojis)
	}
	readyClient := dial(t, bob)
	var ready gateway.Ready
	decode(t, readyClient.waitFor(gateway.EventReady).D, &ready)
	if len(ready.Guilds) != 1 || len(ready.Guilds[0].Emojis) != 1 || ready.Guilds[0].Emojis[0].ID != emoji.ID {
		t.Fatalf("READY guilds = %+v, want emoji", ready.Guilds)
	}
	if _, err = e.m.Guilds.RenameEmoji(t.Context(), guildID, owner, emoji.ID, "new"); err != nil {
		t.Fatalf("rename emoji: %v", err)
	}
	decode(t, bcl.waitFor(events.GuildEmojisUpdate).D, &update)
	if len(update.Emojis) != 1 || update.Emojis[0].Name != "new" {
		t.Fatalf("rename update = %+v, want new", update)
	}
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildSync, Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID}})
	var synced gateway.GuildState
	decode(t, bcl.waitFor(events.GuildSync).D, &synced)
	if len(synced.Emojis) != 1 || synced.Emojis[0].Name != "new" {
		t.Fatalf("GUILD_SYNC emoji = %+v, want new", synced.Emojis)
	}
	if err = e.m.Guilds.DeleteEmoji(t.Context(), guildID, owner, emoji.ID); err != nil {
		t.Fatalf("delete emoji: %v", err)
	}
	decode(t, bcl.waitFor(events.GuildEmojisUpdate).D, &update)
	if update.Emojis == nil || len(update.Emojis) != 0 {
		t.Fatalf("delete update = %+v, want []", update)
	}
	// A subsequent event targeted at the outsider is a dispatch barrier.
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildDelete,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{o.id(t)}},
		Payload:  guilds.GuildDeletePayload{ID: guildID}})
	for {
		f := ocl.next()
		if f.T == events.GuildEmojisUpdate {
			t.Fatalf("non-member received emoji update: %s", f.D)
		}
		if f.T == events.GuildDelete {
			break
		}
	}
}
