package guilds_test

import (
	"slices"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestProfileLink(t *testing.T) {
	e := newEnv(t)
	alice, au := e.user("alice")
	_, bu := e.user("bob")
	_, cu := e.user("carol")
	_, du := e.user("dave")
	g1 := createGuild(t, alice, "first")
	g2 := createGuild(t, alice, "second")
	e.join(g1.id(t), bu.id(t))
	e.join(g2.id(t), bu.id(t))
	ctx := t.Context()
	rel := func(a, b snowflake.ID, typ int) {
		t.Helper()
		if _, err := e.pool.Exec(ctx, `INSERT INTO relationships (user_id, other_id, type, since) VALUES ($1, $2, $3, $4)`,
			a, b, typ, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	rel(au.id(t), cu.id(t), 3) // carol asked alice to be friends
	rel(au.id(t), du.id(t), 2) // alice blocked dave

	for _, tc := range []struct {
		name           string
		viewer, target snowflake.ID
		mutual         []snowflake.ID
		visible        bool
	}{
		{"two shared guilds", au.id(t), bu.id(t), sorted(g1.id(t), g2.id(t)), true},
		{"a pending request", au.id(t), cu.id(t), nil, true},
		{"blocked is not a link", au.id(t), du.id(t), nil, false},
		{"strangers", bu.id(t), cu.id(t), nil, false},
		{"oneself", du.id(t), du.id(t), nil, true},
	} {
		mutual, visible, err := e.svc.ProfileLink(ctx, tc.viewer, tc.target)
		if err != nil {
			t.Fatal(err)
		}
		if visible != tc.visible || !slices.Equal(mutual, tc.mutual) {
			t.Errorf("%s: ProfileLink = %v, %v; want %v, %v", tc.name, mutual, visible, tc.mutual, tc.visible)
		}
	}
}
