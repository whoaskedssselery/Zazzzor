package messages_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func mentionMessage(t *testing.T, c *testkit.Client, ch, content string) messages.PublicMessage {
	t.Helper()
	r := send(c, ch, map[string]any{"content": content})
	expect(t, r, http.StatusCreated)
	var msg messages.PublicMessage
	r.JSON(&msg)
	return msg
}

func readMarkers(t *testing.T, e *env, user, ch string) []messages.ReadState {
	t.Helper()
	got, err := e.msgs.ReadStates(t.Context(), parseID(t, user), []snowflake.ID{parseID(t, ch)})
	if err != nil {
		t.Fatalf("read states: %v", err)
	}
	return got
}

func requireMarker(t *testing.T, e *env, user, ch, last string, count int32) {
	t.Helper()
	got := readMarkers(t, e, user, ch)
	if len(got) != 1 || got[0].LastReadID.String() != last || got[0].MentionCount != count {
		t.Fatalf("state of %s = %+v, want last %s and count %d", user, got, last, count)
	}
}

func TestDirectMentionCountsOnlyMembersAndNeverTheAuthor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	_, b := e.user("bob")
	_, c := e.user("carol")
	_, outside := e.user("outside")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	e.join(g, c)
	content := fmt.Sprintf("<@%s> <@%s> <@%s> <@%s>", b.ID, b.ID, outside.ID, a.ID)
	msg := mentionMessage(t, alice, ch.ID, content)
	if want := []snowflake.ID{parseID(t, b.ID), parseID(t, a.ID)}; msg.Content != content || !slices.Equal(msg.MentionUserIDs, want) {
		t.Fatalf("message = %+v, want original text and IDs %v", msg, want)
	}
	requireMarker(t, e, b.ID, ch.ID, "0", 1)
	requireMarker(t, e, c.ID, ch.ID, "0", 0)
	requireMarker(t, e, a.ID, ch.ID, msg.ID.String(), 0)
	var selfRecipient bool
	err := e.pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM message_mention_recipients WHERE message_id = $1 AND user_id = $2)",
		int64(msg.ID), int64(parseID(t, a.ID))).Scan(&selfRecipient)
	if err != nil || selfRecipient {
		t.Fatalf("self recipient = %v, error = %v, want false and nil", selfRecipient, err)
	}
}

func TestMentionLimitCountsOnlyDistinctValidMembers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	g, ch, _ := newGuild(t, alice, "club")
	const fixtureStart = 1000
	_, err := e.pool.Exec(t.Context(), `INSERT INTO users (id,username,display_name,password_hash,created_at)
		SELECT id, 'fixture_' || id, 'fixture', '', now()
		FROM generate_series($1::bigint, $2::bigint) AS id`, fixtureStart, fixtureStart+messages.MaxMentions)
	if err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	_, err = e.pool.Exec(t.Context(), `INSERT INTO members (guild_id,user_id,joined_at)
		SELECT $1, id, now() FROM generate_series($2::bigint, $3::bigint) AS id`,
		int64(parseID(t, g.ID)), fixtureStart, fixtureStart+messages.MaxMentions)
	if err != nil {
		t.Fatalf("seed members: %v", err)
	}
	var text strings.Builder
	text.WriteString("<@999> ")
	var want []snowflake.ID
	for i := range messages.MaxMentions + 1 {
		id := snowflake.ID(fixtureStart + i)
		fmt.Fprintf(&text, "<@%s> <@%s> ", id, id)
		if i < messages.MaxMentions {
			want = append(want, id)
		}
	}
	msg := mentionMessage(t, alice, ch.ID, text.String())
	if !slices.Equal(msg.MentionUserIDs, want) {
		t.Fatalf("mention IDs = %v, want %v", msg.MentionUserIDs, want)
	}
}

func TestRoleMentionUsesAuthorAndRecipientPermissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	sender, a := e.user("sender")
	_, b := e.user("bob")
	_, c := e.user("carol")
	g, ch, _ := newGuild(t, owner, "club")
	for _, u := range []userBody{a, b, c} {
		e.join(g, u)
	}
	base := "/api/v1/guilds/" + g.ID
	r := owner.Do(http.MethodPost, base+"/roles", map[string]string{"name": "helpers"})
	expect(t, r, http.StatusCreated)
	var role struct{ ID string }
	r.JSON(&role)
	for _, u := range []userBody{b, c} {
		expect(t, owner.Do(http.MethodPatch, base+"/members/"+u.ID, map[string]any{"roles": []string{role.ID}}), http.StatusOK)
	}
	denyEveryone(t, owner, g, ch.ID, permissions.MentionEveryone)
	expect(t, owner.Do(http.MethodPut, "/api/v1/channels/"+ch.ID+"/permissions/"+c.ID,
		map[string]any{"type": 1, "allow": "0", "deny": perm(permissions.ViewChannel)}), http.StatusOK)
	content := "<@&" + role.ID + "> <@&" + g.ID + ">"
	msg := mentionMessage(t, sender, ch.ID, content+" @everyone")
	if len(msg.MentionRoleIDs) != 0 || msg.MentionEveryone {
		t.Fatalf("unauthorized mentions = %+v, want none", msg)
	}
	msg = mentionMessage(t, owner, ch.ID, content)
	if want := []snowflake.ID{parseID(t, role.ID)}; !slices.Equal(msg.MentionRoleIDs, want) {
		t.Fatalf("authorized role IDs = %v, want %v", msg.MentionRoleIDs, want)
	}
	requireMarker(t, e, b.ID, ch.ID, "0", 1)
	if got := readMarkers(t, e, c.ID, ch.ID); len(got) != 0 {
		t.Fatalf("hidden channel read state = %v, want no rows", got)
	}
	expect(t, owner.Do(http.MethodPatch, base+"/roles/"+role.ID, map[string]bool{"mentionable": true}), http.StatusOK)
	msg = mentionMessage(t, sender, ch.ID, content)
	if len(msg.MentionRoleIDs) != 1 {
		t.Fatalf("mentionable role IDs = %v, want the helpers role", msg.MentionRoleIDs)
	}
	requireMarker(t, e, b.ID, ch.ID, "0", 2)
}

func TestHereCountsOnlyOnlineMembers(t *testing.T) {
	t.Parallel()
	var online atomic.Int64
	e := newEnv(t, func(o *messages.Options) {
		o.OnlineAmong = func([]snowflake.ID) []snowflake.ID { return []snowflake.ID{snowflake.ID(online.Load())} }
	})
	alice, a := e.user("alice")
	_, b := e.user("bob")
	_, c := e.user("carol")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	e.join(g, c)
	online.Store(int64(parseID(t, b.ID)))
	here := mentionMessage(t, alice, ch.ID, "@here")
	if !here.MentionEveryone || !here.MentionHere {
		t.Fatalf("@here flags = %v, %v, want both true", here.MentionEveryone, here.MentionHere)
	}
	requireMarker(t, e, b.ID, ch.ID, "0", 1)
	requireMarker(t, e, c.ID, ch.ID, "0", 0)
	requireMarker(t, e, a.ID, ch.ID, here.ID.String(), 0)
	all := mentionMessage(t, alice, ch.ID, "@here and @everyone")
	if !all.MentionEveryone || all.MentionHere {
		t.Fatalf("combined flags = %v, %v, want everyone only", all.MentionEveryone, all.MentionHere)
	}
	requireMarker(t, e, c.ID, ch.ID, "0", 1)
}

func TestAckRecomputesMentionsAfterEditAndDelete(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	first := post(t, alice, ch.ID, "first")
	second := post(t, alice, ch.ID, "<@"+b.ID+">")
	third := post(t, alice, ch.ID, "<@"+b.ID+">")
	fourth := post(t, alice, ch.ID, "fourth")
	requireMarker(t, e, b.ID, ch.ID, "0", 2)
	r := alice.Do(http.MethodPatch, messagePath(ch.ID, second.ID), map[string]string{"content": "quiet"})
	expect(t, r, http.StatusOK)
	var edited messages.PublicMessage
	r.JSON(&edited)
	if len(edited.MentionUserIDs) != 0 {
		t.Fatalf("edited mention IDs = %v, want none", edited.MentionUserIDs)
	}
	expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, third.ID), nil), http.StatusNoContent)
	r = alice.Do(http.MethodPatch, messagePath(ch.ID, fourth.ID), map[string]string{"content": "<@" + b.ID + ">"})
	expect(t, r, http.StatusOK)
	requireMarker(t, e, b.ID, ch.ID, "0", 2)
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, first.ID)+"/ack", nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, first.ID, 1)
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, fourth.ID)+"/ack", nil), http.StatusNoContent)
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, second.ID)+"/ack", nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, fourth.ID, 0)
	ev := e.pub.ofType(events.MessageAck)
	if len(ev) != 2 || ev[0].Audience.Kind != events.AudienceUsers ||
		!slices.Equal(ev[0].Audience.UserIDs, []snowflake.ID{parseID(t, b.ID)}) {
		t.Fatalf("ACK events = %+v, want two personal updates", ev)
	}
}

func TestGuildAckOnlyMovesVisibleChannels(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	hidden := createChannel(t, alice, g, "hidden")
	denyEveryone(t, alice, g, hidden.ID, permissions.ViewChannel)
	denyEveryone(t, alice, g, ch.ID, permissions.ReadMessageHistory)
	m := post(t, alice, ch.ID, "visible")
	h := post(t, alice, hidden.ID, "hidden")
	problemOf(t, bob.Do(http.MethodPost, messagePath(ch.ID, m.ID)+"/ack", nil), http.StatusForbidden, "missing_permission")
	problemOf(t, bob.Do(http.MethodPost, messagePath(hidden.ID, h.ID)+"/ack", nil), http.StatusNotFound, "not_found")
	expect(t, bob.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/ack", nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, m.ID, 0)
	if got := readMarkers(t, e, b.ID, hidden.ID); len(got) != 0 {
		t.Fatalf("hidden markers = %v, want no rows", got)
	}
}

func TestConcurrentMentionsAndNonceRetryCountOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, a := e.user("alice")
	_, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	authorID, chID := parseID(t, a.ID), parseID(t, ch.ID)
	const sends = 8
	var wg sync.WaitGroup
	var errs [sends]error
	start := make(chan struct{})
	for i := range sends {
		wg.Go(func() {
			<-start
			_, errs[i] = e.msgs.Create(t.Context(), chID, authorID, messages.CreateFields{Content: "<@" + b.ID + ">"})
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent send %d: %v", i, err)
		}
	}
	nonce := "read-once"
	fields := messages.CreateFields{Content: "<@" + b.ID + ">", Nonce: &nonce}
	for range 2 {
		if _, err := e.msgs.Create(t.Context(), chID, authorID, fields); err != nil {
			t.Fatalf("send with nonce: %v", err)
		}
	}
	requireMarker(t, e, b.ID, ch.ID, "0", sends+1)
}

func TestAckRefreshesUnchangedPositionAfterDeletingLatest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	seen := post(t, alice, ch.ID, "seen")
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, seen.ID)+"/ack", nil), http.StatusNoContent)
	unread := post(t, alice, ch.ID, "<@"+b.ID+">")
	expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, unread.ID), nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, seen.ID, 1)
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, seen.ID)+"/ack", nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, seen.ID, 0)
}

func TestGuildAckClearsDeletedMentionsWithoutRegressingReadPosition(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	seen := post(t, alice, ch.ID, "seen")
	expect(t, bob.Do(http.MethodPost, messagePath(ch.ID, seen.ID)+"/ack", nil), http.StatusNoContent)
	unread := post(t, alice, ch.ID, "<@"+b.ID+">")
	for _, id := range []string{seen.ID, unread.ID} {
		expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, id), nil), http.StatusNoContent)
	}
	requireMarker(t, e, b.ID, ch.ID, seen.ID, 1)
	expect(t, bob.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/ack", nil), http.StatusNoContent)
	requireMarker(t, e, b.ID, ch.ID, seen.ID, 0)
}

func TestChannelNewestMessageFollowsSendAndDelete(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, ch, _ := newGuild(t, alice, "club")
	head := func() *string {
		t.Helper()
		var channel struct {
			Last *string `json:"last_message_id"`
		}
		r := alice.Do(http.MethodGet, "/api/v1/channels/"+ch.ID, nil)
		expect(t, r, http.StatusOK)
		r.JSON(&channel)
		return channel.Last
	}
	a := post(t, alice, ch.ID, "one")
	b := post(t, alice, ch.ID, "two")
	if got := head(); got == nil || *got != b.ID {
		t.Fatalf("head after send = %v, want %s", got, b.ID)
	}
	expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, b.ID), nil), http.StatusNoContent)
	if got := head(); got == nil || *got != a.ID {
		t.Fatalf("head after newest delete = %v, want %s", got, a.ID)
	}
	ev := e.pub.ofType(events.MessageDelete)
	if len(ev) != 1 {
		t.Fatalf("delete events = %d, want one", len(ev))
	}
	d, ok := ev[0].Payload.(messages.DeletePayload)
	if !ok || d.LastMessageID == nil || d.LastMessageID.String() != a.ID {
		t.Fatalf("delete event payload = %+v, want new head %s", ev[0].Payload, a.ID)
	}
	expect(t, alice.Do(http.MethodDelete, messagePath(ch.ID, a.ID), nil), http.StatusNoContent)
	if got := head(); got != nil {
		t.Fatalf("head of empty channel = %s, want null", *got)
	}
}

func TestMentionAudienceSnapshotSerializesWithGuildPermissionWrites(t *testing.T) {
	t.Parallel()
	atPresence := make(chan struct{})
	continueSend := make(chan struct{})
	resume := sync.OnceFunc(func() { close(continueSend) })
	defer resume()
	e := newEnv(t, func(o *messages.Options) {
		o.OnlineAmong = func(ids []snowflake.ID) []snowflake.ID {
			close(atPresence)
			<-continueSend
			return ids
		}
	})
	alice, a := e.user("alice")
	_, b := e.user("bob")
	g, ch, _ := newGuild(t, alice, "club")
	e.join(g, b)
	authorID, channelID := parseID(t, a.ID), parseID(t, ch.ID)
	sent := make(chan error, 1)
	go func() {
		_, err := e.msgs.Create(t.Context(), channelID, authorID, messages.CreateFields{Content: "@here"})
		sent <- err
	}()
	select {
	case <-atPresence:
	case err := <-sent:
		t.Fatalf("send returned before capturing presence: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("send did not reach presence")
	}
	// Permission edits take this exact lock. A separate connection must be
	// unable to acquire it while the sender is collecting its audience.
	tx, err := e.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin competing transaction: %v", err)
	}
	var id int64
	err = tx.QueryRow(t.Context(), "SELECT id FROM guilds WHERE id = $1 FOR NO KEY UPDATE NOWAIT", int64(parseID(t, g.ID))).Scan(&id)
	_ = tx.Rollback(t.Context())
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "55P03" {
		t.Errorf("competing guild lock error = %v, want lock_not_available", err)
	}
	resume()
	if err = <-sent; err != nil {
		t.Fatalf("send after releasing presence: %v", err)
	}
	requireMarker(t, e, b.ID, ch.ID, "0", 1)
}
