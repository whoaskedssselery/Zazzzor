package notifysettings_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/notifysettings"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

const (
	alice snowflake.ID = 101
	bob   snowflake.ID = 102
	home  snowflake.ID = 10 // Alice's server
	other snowflake.ID = 20 // another server Alice is in
	text  snowflake.ID = 11 // a channel of home
	away  snowflake.ID = 21 // a channel of other
	dm    snowflake.ID = 30 // Alice and Bob's DM
)

// scope answers like channels.Service: the server of a visible channel, 0
// for a DM the user is in, ErrNotFound otherwise.
type scope struct{ hidden map[snowflake.ID]bool }

func (f scope) Permissions(_ context.Context, _, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	switch {
	case f.hidden[channelID]:
		return 0, 0, channels.ErrNotFound
	case channelID == dm:
		return 0, 0, nil
	case channelID == away:
		return 0, other, nil
	default:
		return 0, home, nil
	}
}

type recorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *recorder) Publish(_ context.Context, e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

var now = time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)

func setup(t *testing.T, hidden ...snowflake.ID) (*notifysettings.Service, *pgxpool.Pool, *recorder) {
	t.Helper()
	pool := testkit.Postgres(t)
	_, err := pool.Exec(t.Context(), `
INSERT INTO users(id,username,display_name,password_hash,created_at)
 VALUES (101,'alice','Alice','unused',now()),(102,'bob','Bob','unused',now());
INSERT INTO guilds(id,name,owner_id,created_at) VALUES (10,'home',101,now()),(20,'other',102,now());
INSERT INTO members(guild_id,user_id,joined_at) VALUES (10,101,now()),(20,101,now()),(20,102,now());
INSERT INTO channels(id,guild_id,type,name,position,permission_synced,created_at)
 VALUES (11,10,0,'text',0,false,now()),(21,20,0,'away',0,false,now());
INSERT INTO channels(id,type,position,permission_synced,created_at) VALUES (30,1,0,false,now());
INSERT INTO channel_recipients(channel_id,user_id,joined_at) VALUES (30,101,now()),(30,102,now());`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := map[snowflake.ID]bool{}
	for _, id := range hidden {
		h[id] = true
	}
	pub := &recorder{}
	s := notifysettings.NewService(notifysettings.Options{Pool: pool, Channels: scope{hidden: h}, Publisher: pub,
		Now: func() time.Time { return now }})
	return s, pool, pub
}

func ptr[T any](v T) *T { return &v }

func until(d time.Duration) notifysettings.Optional[time.Time] {
	t := now.Add(d)
	return notifysettings.Optional[time.Time]{Set: true, Value: &t}
}

func requireProblem(t *testing.T, err error, status int, code string) {
	t.Helper()
	var p *httpx.Problem
	if !errors.As(err, &p) || p.Status != status || p.Code != code {
		t.Fatalf("error = %v, want %d %s", err, status, code)
	}
}

func g(id snowflake.ID) *snowflake.ID { return &id }

func TestServerMuteLevelsAndEvent(t *testing.T) {
	s, _, pub := setup(t)
	got, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{Muted: ptr(true), MuteUntil: until(time.Hour),
		MessageNotifications: ptr(notifysettings.Nothing), SuppressEveryone: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Muted || got.MuteUntil == nil || got.MessageNotifications != 2 || !got.SuppressEveryone || *got.GuildID != home {
		t.Fatalf("settings = %+v", got)
	}
	if len(pub.events) != 1 || pub.events[0].Type != notifysettings.EventUpdate ||
		pub.events[0].Audience.Kind != events.AudienceUsers || pub.events[0].Audience.UserIDs[0] != alice {
		t.Fatalf("events = %+v, want one to Alice", pub.events)
	}
	// Unmuting drops the end too; back to the defaults stores nothing.
	got, err = s.Update(t.Context(), alice, g(home), notifysettings.Patch{Muted: ptr(false),
		MessageNotifications: ptr(notifysettings.OnlyMentions), SuppressEveryone: ptr(false)})
	if err != nil || got.Muted || got.MuteUntil != nil {
		t.Fatalf("after unmute = %+v, %v", got, err)
	}
	list, err := s.List(t.Context(), alice)
	if err != nil || len(list) != 0 {
		t.Fatalf("list = %+v, %v; want nothing (defaults)", list, err)
	}
}

func TestSettingsVersionSurvivesReturningToDefaults(t *testing.T) {
	s, _, pub := setup(t)
	version := func(value any) int64 {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal settings: %v", err)
		}
		var snapshot struct {
			Version int64 `json:"version"`
		}
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatalf("decode settings version: %v", err)
		}
		return snapshot.Version
	}
	var last int64
	for _, muted := range []bool{true, false, true} {
		got, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{Muted: ptr(muted)})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		current := version(got)
		if current <= last {
			t.Fatalf("version = %d, want greater than %d", current, last)
		}
		if event := version(pub.events[len(pub.events)-1].Payload); event != current {
			t.Errorf("event version = %d, want %d", event, current)
		}
		last = current
	}
	list, err := s.List(t.Context(), alice)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v, want one scope", list, err)
	}
	if ready := version(list[0]); ready != last {
		t.Errorf("READY version = %d, want %d", ready, last)
	}
}

func TestChannelOverridesMergeAndCheckScope(t *testing.T) {
	s, _, _ := setup(t, 99)
	_, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
		text: {Muted: ptr(true)}}})
	if err != nil {
		t.Fatal(err)
	}
	// A later PATCH that names no channel keeps the override.
	got, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{MessageNotifications: ptr(notifysettings.AllMessages)})
	if err != nil || len(got.ChannelOverrides) != 1 || !got.ChannelOverrides[0].Muted ||
		got.ChannelOverrides[0].MessageNotifications != notifysettings.Inherit {
		t.Fatalf("after merge = %+v, %v", got, err)
	}
	got, err = s.Update(t.Context(), alice, g(home), notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
		text: {Muted: ptr(false)}}})
	if err != nil || len(got.ChannelOverrides) != 0 {
		t.Fatalf("an override back at the defaults stayed: %+v, %v", got, err)
	}
	for _, ch := range []snowflake.ID{away, 99} {
		_, err = s.Update(t.Context(), alice, g(home), notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
			ch: {Muted: ptr(true)}}})
		requireProblem(t, err, 400, "unknown_channel")
	}
}

func TestPrivateScope(t *testing.T) {
	s, _, _ := setup(t)
	got, err := s.Update(t.Context(), alice, nil, notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
		dm: {Muted: ptr(true)}}})
	if err != nil || got.GuildID != nil || got.MessageNotifications != notifysettings.AllMessages ||
		len(got.ChannelOverrides) != 1 || got.ChannelOverrides[0].ChannelID != dm {
		t.Fatalf("@me = %+v, %v", got, err)
	}
	_, err = s.Update(t.Context(), alice, nil, notifysettings.Patch{SuppressEveryone: ptr(true)})
	requireProblem(t, err, 400, "validation_failed")
	_, err = s.Update(t.Context(), alice, nil, notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
		text: {Muted: ptr(true)}}})
	requireProblem(t, err, 400, "unknown_channel")
}

func TestRefusals(t *testing.T) {
	s, _, _ := setup(t)
	_, err := s.Update(t.Context(), bob, g(home), notifysettings.Patch{Muted: ptr(true)})
	requireProblem(t, err, 404, "not_found")
	for name, p := range map[string]notifysettings.Patch{
		"past":       {Muted: ptr(true), MuteUntil: until(-time.Minute)},
		"too far":    {Muted: ptr(true), MuteUntil: until(31 * 24 * time.Hour)},
		"not muted":  {MuteUntil: until(time.Hour)},
		"bad level":  {MessageNotifications: ptr(int16(3))},
		"bad level2": {ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{text: {MessageNotifications: ptr(int16(4))}}},
	} {
		_, err = s.Update(t.Context(), alice, g(home), p)
		requireProblem(t, err, 400, "validation_failed")
		_ = name
	}
}

func TestReadyListAndCleanup(t *testing.T) {
	s, pool, _ := setup(t)
	if _, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{ChannelOverrides: map[snowflake.ID]notifysettings.OverridePatch{
		text: {MessageNotifications: ptr(notifysettings.Nothing)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(t.Context(), alice, g(other), notifysettings.Patch{Muted: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(t.Context(), alice, nil, notifysettings.Patch{MessageNotifications: ptr(notifysettings.Nothing)}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(t.Context(), alice)
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %+v, %v; want home (override), other, @me", list, err)
	}
	// Deleting the channel drops its override; leaving the server drops its settings.
	if _, err = pool.Exec(t.Context(), `DELETE FROM channels WHERE id = 11; DELETE FROM members WHERE guild_id = 20 AND user_id = 101`); err != nil {
		t.Fatal(err)
	}
	list, err = s.List(t.Context(), alice)
	if err != nil || len(list) != 1 || list[0].GuildID != nil {
		t.Fatalf("after cleanup = %+v, %v; want @me alone", list, err)
	}
}

func TestOverrideCapAndRateLimit(t *testing.T) {
	s, pool, _ := setup(t)
	if _, err := pool.Exec(t.Context(), `
INSERT INTO channels(id,guild_id,type,name,position,permission_synced,created_at)
 SELECT 1000 + n, 10, 0, 'c' || n, n + 1, false, now() FROM generate_series(1, 501) AS n`); err != nil {
		t.Fatal(err)
	}
	many := map[snowflake.ID]notifysettings.OverridePatch{}
	for n := 1; n <= 501; n++ {
		many[snowflake.ID(1000+n)] = notifysettings.OverridePatch{Muted: ptr(true)}
	}
	_, err := s.Update(t.Context(), alice, g(home), notifysettings.Patch{ChannelOverrides: many})
	requireProblem(t, err, 400, "too_many_overrides")
	for range 29 {
		if _, err = s.Update(t.Context(), alice, g(home), notifysettings.Patch{}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.Update(t.Context(), alice, g(home), notifysettings.Patch{})
	requireProblem(t, err, 429, "rate_limited")
}
