package messages_test

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

func TestCustomReactionSurvivesRenameAndDelete(t *testing.T) {
	t.Parallel()
	e, owner, alice, bob, g, m := reactionFixture(t)
	ownerID := parseID(t, m.Author.ID)
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode emoji: %v", err)
	}
	emoji, err := e.guilds.CreateEmoji(t.Context(), parseID(t, g.ID), ownerID, "old", raw.Bytes())
	if err != nil {
		t.Fatalf("create emoji: %v", err)
	}
	id := emoji.ID.String()
	original := reactionPath(m, "old:"+id)
	expect(t, alice.Do(http.MethodPut, original+"/@me", nil), http.StatusNoContent)
	if _, err = e.guilds.RenameEmoji(t.Context(), emoji.GuildID, ownerID, emoji.ID, "new"); err != nil {
		t.Fatalf("rename emoji: %v", err)
	}
	renamed := reactionPath(m, "new:"+id)
	expect(t, bob.Do(http.MethodPut, renamed+"/@me", nil), http.StatusNoContent)
	expect(t, alice.Do(http.MethodPut, renamed+"/@me", nil), http.StatusNoContent)
	check := func(count int) {
		t.Helper()
		got := reactionSummary(t, alice, m)
		if len(got) != 1 || got[0].Count != count || got[0].Emoji.ID == nil || *got[0].Emoji.ID != id || got[0].Emoji.Name != "new" {
			t.Fatalf("reaction summary = %+v, want %s/new count %d", got, id, count)
		}
	}
	check(2)
	added := e.pub.ofType(events.MessageReactionAdd)
	if len(added) != 2 {
		t.Fatalf("add events = %d, want 2", len(added))
	}
	last := added[1].Payload.(messages.ReactionPayload)
	if last.Emoji.ID == nil || *last.Emoji.ID != emoji.ID || last.Emoji.Name != "new" {
		t.Errorf("added emoji = %+v, want renamed identity", last.Emoji)
	}
	if err = e.guilds.DeleteEmoji(t.Context(), emoji.GuildID, ownerID, emoji.ID); err != nil {
		t.Fatalf("delete emoji: %v", err)
	}
	check(2)
	expect(t, owner.Do(http.MethodPut, renamed+"/@me", nil), http.StatusBadRequest)
	expect(t, alice.Do(http.MethodGet, original, nil), http.StatusOK)
	expect(t, bob.Do(http.MethodDelete, original+"/@me", nil), http.StatusNoContent)
	check(1)
}

func TestCustomReactionRejectsAnotherGuild(t *testing.T) {
	t.Parallel()
	e, owner, alice, _, _, m := reactionFixture(t)
	other, _, _ := newGuild(t, owner, "other")
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode emoji: %v", err)
	}
	emoji, err := e.guilds.CreateEmoji(t.Context(), parseID(t, other.ID), parseID(t, m.Author.ID), "elsewhere", raw.Bytes())
	if err != nil {
		t.Fatalf("create emoji: %v", err)
	}
	path := reactionPath(m, emoji.Name+":"+emoji.ID.String())
	expect(t, alice.Do(http.MethodPut, path+"/@me", nil), http.StatusBadRequest)
	expect(t, alice.Do(http.MethodGet, path, nil), http.StatusBadRequest)
	expect(t, alice.Do(http.MethodDelete, path+"/@me", nil), http.StatusBadRequest)
}
