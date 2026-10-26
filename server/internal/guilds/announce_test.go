package guilds_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// flakyUsers fails every account lookup while fail is set.
type flakyUsers struct {
	guilds.Users
	fail atomic.Bool
}

func (u *flakyUsers) UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]auth.User, error) {
	if u.fail.Load() {
		return nil, errors.New("users: boom")
	}
	return u.Users.UsersByIDs(ctx, ids)
}

func (u *flakyUsers) UsersByIDsIn(ctx context.Context, dbtx auth.DB, ids []snowflake.ID) ([]auth.User, error) {
	if u.fail.Load() {
		return nil, errors.New("users: boom")
	}
	return u.Users.UsersByIDsIn(ctx, dbtx, ids)
}

// A join that is stored is announced even when the snapshot cannot be
// read: the gateway then builds the state itself (GUILD_CREATE for the new
// member, GUILD_SYNC for the rest), or makes them reconnect.
func TestJoinAnnouncedWhenTheSnapshotFails(t *testing.T) {
	users := &flakyUsers{}
	e := newEnv(t, func(o *guilds.Options) {
		users.Users = o.Users
		o.Users = users
	})
	owner, _ := e.user("owner")
	g := createGuild(t, owner, "home")
	_, u := e.user("newbie")
	e.pub.reset()

	users.fail.Store(true)
	if err := e.svc.Join(t.Context(), g.id(t), u.id(t)); err != nil {
		t.Fatalf("join: %v", err)
	}

	created := e.pub.ofType(events.GuildCreate)
	if len(created) != 1 || created[0].Audience.Kind != events.AudienceUsers ||
		len(created[0].Audience.UserIDs) != 1 || created[0].Audience.UserIDs[0] != u.id(t) {
		t.Fatalf("GUILD_CREATE = %+v", created)
	}
	if p, ok := created[0].Payload.(guilds.GuildSyncPayload); !ok || p.GuildID != g.id(t) {
		t.Fatalf("GUILD_CREATE payload = %#v", created[0].Payload)
	}
	synced := e.pub.ofType(events.GuildSync)
	if len(synced) != 1 || synced[0].Audience.Kind != events.AudienceGuildSync ||
		synced[0].Audience.GuildID != g.id(t) || synced[0].ExcludeUserID != u.id(t) {
		t.Fatalf("GUILD_SYNC = %+v", synced)
	}
	if n := len(e.pub.ofType(events.GuildMemberAdd)); n != 0 {
		t.Fatalf("%d GUILD_MEMBER_ADD without a member to show", n)
	}
}
