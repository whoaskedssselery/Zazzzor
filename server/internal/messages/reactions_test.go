package messages_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const thumb = "👍"

type reactionBody struct {
	Emoji struct {
		ID   *string
		Name string
	}
	Count int
	Me    bool
}

// reactionFixture creates a message with two visible members and one owner.
func reactionFixture(t *testing.T, options ...func(*messages.Options)) (*env, *testkit.Client, *testkit.Client, *testkit.Client, guildBody, messageBody) {
	t.Helper()
	e := newEnv(t, options...)
	owner, _ := e.user("owner")
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, ch, _ := newGuild(t, owner, "Reactions")
	e.join(g, a)
	e.join(g, b)
	return e, owner, alice, bob, g, post(t, owner, ch.ID, "hello")
}

// reactionPath encodes the emoji as a single path segment.
func reactionPath(m messageBody, emoji string) string {
	return messagePath(m.ChannelID, m.ID) + "/reactions/" + url.PathEscape(emoji)
}

func TestReactionEmojiValidation(t *testing.T) {
	t.Parallel()
	_, _, alice, _, _, m := reactionFixture(t)
	for _, name := range []string{thumb, "👍🏽", string([]rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467}), "🇷🇺", "❤️"} {
		expect(t, alice.Do(http.MethodPut, reactionPath(m, name)+"/@me", nil), 204)
	}
	for _, name := range []string{"a", "👍👍", " ", thumb + strings.Repeat(string(rune(0x0301)), 15), "🇷", "1️⃣", "👍\n", "👍%"} {
		problemOf(t, alice.Do(http.MethodGet, reactionPath(m, name), nil), 400, "validation_failed")
	}
}

func TestReactionJoiningNeedsOnlyHistory(t *testing.T) {
	t.Parallel()
	_, owner, alice, _, g, m := reactionFixture(t)
	denyEveryone(t, owner, g, m.ChannelID, 1<<6)
	p := reactionPath(m, thumb) + "/@me"
	problemOf(t, alice.Do(http.MethodPut, p, nil), 403, "missing_permission")
	expect(t, owner.Do(http.MethodPut, p, nil), 204)
	expect(t, alice.Do(http.MethodPut, p, nil), 204)
	expect(t, alice.Do(http.MethodDelete, p, nil), 204)
}

func TestReactionRoutesRequireHistory(t *testing.T) {
	t.Parallel()
	_, owner, alice, _, g, m := reactionFixture(t)
	denyEveryone(t, owner, g, m.ChannelID, permissions.ReadMessageHistory)
	p := reactionPath(m, thumb)
	problemOf(t, alice.Do(http.MethodGet, p, nil), 403, "missing_permission")
	problemOf(t, alice.Do(http.MethodPut, p+"/@me", nil), 403, "missing_permission")
	problemOf(t, alice.Do(http.MethodDelete, p+"/@me", nil), 403, "missing_permission")
	problemOf(t, alice.Do(http.MethodDelete, p+"/"+m.Author.ID, nil), 403, "missing_permission")
}

func TestReactionWritesAreIdempotent(t *testing.T) {
	t.Parallel()
	e, owner, alice, _, _, m := reactionFixture(t)
	p := reactionPath(m, thumb) + "/@me"
	for range 2 {
		expect(t, alice.Do(http.MethodPut, p, nil), 204)
	}
	if got := len(e.pub.ofType("MESSAGE_REACTION_ADD")); got != 1 {
		t.Errorf("add events = %d, want 1", got)
	}
	if got := reactionSummary(t, owner, m); len(got) != 1 || got[0].Count != 1 {
		t.Errorf("summary = %v, want one reaction", got)
	}
	for range 2 {
		expect(t, alice.Do(http.MethodDelete, p, nil), 204)
	}
	if got := len(e.pub.ofType("MESSAGE_REACTION_REMOVE")); got != 1 {
		t.Errorf("remove events = %d, want 1", got)
	}
	if got := reactionSummary(t, owner, m); len(got) != 0 {
		t.Errorf("summary = %v, want empty", got)
	}
}

func TestModeratingReactions(t *testing.T) {
	t.Parallel()
	e, owner, alice, bob, g, m := reactionFixture(t)
	p := reactionPath(m, thumb)
	expect(t, alice.Do(http.MethodPut, p+"/@me", nil), 204)
	var users []userBody
	resp := owner.Do(http.MethodGet, p, nil)
	expect(t, resp, 200)
	resp.JSON(&users)
	if len(users) != 1 {
		t.Fatalf("reactors = %v, want one", users)
	}
	target := p + "/" + users[0].ID
	problemOf(t, bob.Do(http.MethodDelete, target, nil), 403, "missing_permission")
	overwrite := "/api/v1/channels/" + m.ChannelID + "/permissions/" + g.ID
	expect(t, owner.Do(http.MethodPut, overwrite, map[string]any{
		"type": 0, "allow": perm(permissions.ManageMessages), "deny": "0",
	}), 200)
	problemOf(t, bob.Do(http.MethodDelete, target, nil), 403, "role_hierarchy")
	expect(t, owner.Do(http.MethodDelete, target, nil), 204)
	events := e.pub.ofType("MESSAGE_REACTION_REMOVE")
	if len(events) != 1 {
		t.Fatalf("remove events = %v, want one", events)
	}
	body, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var got struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if got.UserID != users[0].ID {
		t.Errorf("removed user = %s, want %s", got.UserID, users[0].ID)
	}
}

// reactionSummary reads the caller-specific summary from the single-message route.
func reactionSummary(t *testing.T, c *testkit.Client, m messageBody) []reactionBody {
	t.Helper()
	resp := c.Do(http.MethodGet, messagePath(m.ChannelID, m.ID), nil)
	expect(t, resp, 200)
	var body struct{ Reactions []reactionBody }
	resp.JSON(&body)
	if body.Reactions == nil {
		t.Fatal("reactions = nil, want an array")
	}
	return body.Reactions
}

func TestReactionLimitStillAllowsJoining(t *testing.T) {
	t.Parallel()
	_, owner, alice, bob, _, m := reactionFixture(t)
	for i := range 20 {
		c := []*testkit.Client{owner, alice, bob}[i%3]
		expect(t, c.Do(http.MethodPut, reactionPath(m, string(rune(0x1F600+i)))+"/@me", nil), 204)
	}
	problemOf(t, bob.Do(http.MethodPut, reactionPath(m, thumb)+"/@me", nil), 400, "too_many_reactions")
	expect(t, bob.Do(http.MethodPut, reactionPath(m, "😀")+"/@me", nil), 204)
}

func TestReactionSummaryKeepsFirstAdditionOrder(t *testing.T) {
	t.Parallel()
	e, owner, alice, bob, _, m := reactionFixture(t)
	first, second := reactionPath(m, thumb)+"/@me", reactionPath(m, "😂")+"/@me"
	expect(t, alice.Do(http.MethodPut, first, nil), 204)
	expect(t, alice.Do(http.MethodPut, second, nil), 204)
	expect(t, bob.Do(http.MethodPut, first, nil), 204)
	expect(t, alice.Do(http.MethodDelete, first, nil), 204)
	for _, c := range []*testkit.Client{alice, bob} {
		resp := c.Do(http.MethodGet, messagesPath(m.ChannelID), nil)
		expect(t, resp, 200)
		var history []struct{ Reactions []reactionBody }
		resp.JSON(&history)
		if len(history) != 1 {
			t.Fatalf("history = %v, want one message", history)
		}
		got := history[0].Reactions
		if len(got) != 2 || got[0].Emoji.Name != thumb || got[0].Emoji.ID != nil || got[0].Count != 1 || got[0].Me != (c == bob) ||
			got[1].Emoji.Name != "😂" || got[1].Count != 1 || got[1].Me != (c == alice) {
			t.Errorf("summary = %v, want first addition order and caller's reaction", got)
		}
	}
	resp := owner.Do(http.MethodPatch, messagePath(m.ChannelID, m.ID), map[string]string{"content": "edited"})
	expect(t, resp, 200)
	var edited struct{ Reactions []reactionBody }
	resp.JSON(&edited)
	if len(edited.Reactions) != 2 {
		t.Errorf("edited reactions = %v, want two", edited.Reactions)
	}
	updates := e.pub.ofType("MESSAGE_UPDATE")
	if len(updates) != 1 {
		t.Fatalf("updates = %v, want one", updates)
	}
	b, err := json.Marshal(updates[0].Payload)
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	if strings.Contains(string(b), "\"reactions\"") {
		t.Errorf("update payload contains reactions: %s", b)
	}
}

func TestReactionConcurrentAddsAndUserPaging(t *testing.T) {
	t.Parallel()
	_, owner, alice, bob, _, m := reactionFixture(t)
	p := reactionPath(m, thumb)
	t.Run("simultaneous adds", func(t *testing.T) {
		for i, c := range []*testkit.Client{alice, bob} {
			t.Run([]string{"alice", "bob"}[i], func(t *testing.T) {
				t.Parallel()
				if err := doReactionWrite(t.Context(), c, http.MethodPut, p+"/@me"); err != nil {
					t.Errorf("add: %v", err)
				}
			})
		}
	})
	if got := reactionSummary(t, owner, m); len(got) != 1 || got[0].Count != 2 || got[0].Me {
		t.Errorf("summary = %v, want two reactions from other users", got)
	}
	resp := owner.Do(http.MethodGet, p, nil)
	expect(t, resp, 200)
	var all []userBody
	resp.JSON(&all)
	if len(all) != 2 || parseID(t, all[0].ID) >= parseID(t, all[1].ID) {
		t.Fatalf("users = %v, want two in ascending order", all)
	}
	for i, query := range []string{"?limit=1", "?limit=1&after=" + all[0].ID} {
		resp = owner.Do(http.MethodGet, p+query, nil)
		expect(t, resp, 200)
		var page []userBody
		resp.JSON(&page)
		if !slices.Equal(page, all[i:i+1]) {
			t.Errorf("page %d = %v, want %v", i, page, all[i:i+1])
		}
	}
}

type pausedReactionPublish struct {
	recorder
	started chan struct{}
	release chan struct{}
}

// Publish pauses an add after the database has committed, before it reaches the bus.
func (p *pausedReactionPublish) Publish(ctx context.Context, event events.Event) {
	if event.Type == "MESSAGE_REACTION_ADD" {
		close(p.started)
		<-p.release
	}
	p.recorder.Publish(ctx, event)
}

func TestReactionEventsFollowCommitOrder(t *testing.T) {
	t.Parallel()
	pub := &pausedReactionPublish{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(pub.release) })
	defer release()
	_, _, alice, _, _, m := reactionFixture(t, func(o *messages.Options) { o.Publisher = pub })
	path := reactionPath(m, thumb) + "/@me"

	added := make(chan error, 1)
	go func() { added <- doReactionWrite(t.Context(), alice, http.MethodPut, path) }()
	select {
	case <-pub.started:
	case err := <-added:
		t.Fatalf("add returned before publication: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("add did not reach publisher")
	}

	// An inverse write must wait while the add is about to be announced.
	ctx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	err := doReactionWrite(ctx, alice, http.MethodDelete, path)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("concurrent delete error = %v, want deadline exceeded", err)
	}
	release()
	if err := <-added; err != nil {
		t.Fatalf("add: %v", err)
	}
	expect(t, alice.Do(http.MethodDelete, path, nil), 204)
	pub.mu.Lock()
	defer pub.mu.Unlock()
	var got []string
	for _, event := range pub.events {
		if event.Type == "MESSAGE_REACTION_ADD" || event.Type == "MESSAGE_REACTION_REMOVE" {
			got = append(got, event.Type)
		}
	}
	want := []string{"MESSAGE_REACTION_ADD", "MESSAGE_REACTION_REMOVE"}
	if !slices.Equal(got, want) {
		t.Errorf("reaction events = %v, want %v", got, want)
	}
}

// doReactionWrite returns request failures to the caller's goroutine.
func doReactionWrite(ctx context.Context, c *testkit.Client, method, path string) error {
	status, err := reactionWriteStatus(ctx, c, method, path)
	if err != nil {
		return fmt.Errorf("write reaction: %w", err)
	}
	if status != 204 {
		return fmt.Errorf("reaction status = %d, want 204", status)
	}
	return nil
}

// reactionWriteStatus sends one request from a goroutine without calling Fatal.
func reactionWriteStatus(ctx context.Context, c *testkit.Client, method, path string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL()+path, nil)
	if err != nil {
		return 0, fmt.Errorf("build reaction request: %w", err)
	}
	req.Header.Set("Origin", testkit.DefaultOrigin)
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("send reaction request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

func TestReactionRechecksHistoryUnderGuildLock(t *testing.T) {
	t.Parallel()
	e, owner, alice, _, g, m := reactionFixture(t)
	denyEveryone(t, owner, g, m.ChannelID, 0)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin permission update: %v", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	guildID := parseID(t, g.ID)
	channelID := parseID(t, m.ChannelID)
	if _, lockErr := tx.Exec(ctx, "SELECT id FROM guilds WHERE id = $1 FOR NO KEY UPDATE", guildID); lockErr != nil {
		t.Fatalf("lock guild: %v", lockErr)
	}
	if _, updateErr := tx.Exec(ctx, `
		UPDATE overwrites SET deny = $3 WHERE channel_id = $1 AND target_id = $2
	`, channelID, guildID, int64(permissions.ReadMessageHistory)); updateErr != nil {
		t.Fatalf("deny message history: %v", updateErr)
	}
	pid := int32(tx.Conn().PgConn().PID()) //nolint:gosec // G115: a PostgreSQL process ID is a signed integer.
	path := reactionPath(m, thumb) + "/@me"
	type outcome struct {
		status int
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		status, requestErr := reactionWriteStatus(ctx, alice, http.MethodPut, path)
		done <- outcome{status, requestErr}
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if queryErr := e.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT FROM pg_stat_activity
				WHERE $1 = ANY(pg_blocking_pids(pid))
			)
		`, pid).Scan(&waiting); queryErr != nil {
			t.Fatalf("find blocked reaction transaction: %v", queryErr)
		}
		if waiting {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("reaction returned before guild unlock: %+v", got)
		case <-ctx.Done():
			t.Fatalf("reaction did not wait for guild lock: %v", ctx.Err())
		case <-tick.C:
		}
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit permission update: %v", commitErr)
	}
	if got := <-done; got.status != 403 || got.err != nil {
		t.Errorf("reaction after permission revocation = %+v, want 403", got)
	}
}
