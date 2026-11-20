package gateway_test

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// The events guilds.AnnounceJoin falls back to when it cannot read the
// snapshot still give the new member their own state and the others the
// new member list.
func TestJoinAnnouncedWithoutSnapshot(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general := newGuild(t, alice, "guild")
	acl, _ := connect(t, alice)
	bcl, _ := connect(t, bob)

	guildID := parseID(t, g.ID)
	if err := db.InTx(t.Context(), e.pool, func(tx pgx.Tx) error {
		_, err := e.m.Guilds.AddMember(t.Context(), tx, guildID, bu.id(t))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildSync,
		Audience: events.Audience{Kind: events.AudienceGuildSync, GuildID: guildID},
		Payload:  guilds.GuildSyncPayload{GuildID: guildID}, ExcludeUserID: bu.id(t)})
	e.bus.Publish(t.Context(), events.Event{Type: events.GuildCreate,
		Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{bu.id(t)}},
		Payload:  guilds.GuildSyncPayload{GuildID: guildID}})

	var st guildStateBody
	decode(t, bcl.waitFor(events.GuildCreate).D, &st)
	if st.Guild.ID != g.ID || st.MyMember.User.ID != bu.ID || !hasChannel(st.Channels, general.ID) {
		t.Fatalf("GUILD_CREATE = %+v", st)
	}
	decode(t, acl.waitFor(events.GuildSync).D, &st)
	if !slices.ContainsFunc(st.Members, func(m memberBody) bool { return m.User.ID == bu.ID }) {
		t.Fatalf("GUILD_SYNC members = %+v, want bob", st.Members)
	}
}
