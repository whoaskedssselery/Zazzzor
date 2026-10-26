package messages_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

// flakyUsers fails UsersByIDs while fail is set.
type flakyUsers struct {
	messages.Users
	fail atomic.Bool
}

func (u *flakyUsers) UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]auth.User, error) {
	if u.fail.Load() {
		return nil, errors.New("users: boom")
	}
	return u.Users.UsersByIDs(ctx, ids)
}

func withFlakyUsers(u *flakyUsers) func(*messages.Options) {
	return func(o *messages.Options) {
		u.Users = o.Users
		o.Users = u
	}
}

func (e *env) countMessages(channelID string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(e.t.Context(), "SELECT count(*) FROM messages WHERE channel_id = $1",
		int64(parseID(e.t, channelID))).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// A message is either stored and announced or neither: a failure loading
// the author leaves nothing behind for the client to duplicate on retry.
func TestCreateFailingBeforeTheEventStoresNothing(t *testing.T) {
	users := &flakyUsers{}
	e := newEnv(t, withFlakyUsers(users))
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	users.fail.Store(true)
	expect(t, send(alice, general.ID, map[string]any{"content": "hi"}), http.StatusInternalServerError)
	if n := e.countMessages(general.ID); n != 0 {
		t.Fatalf("%d messages stored without an event", n)
	}
	if n := len(e.pub.ofType(events.MessageCreate)); n != 0 {
		t.Fatalf("%d MESSAGE_CREATE events", n)
	}
}

func TestEditFailingBeforeTheEventChangesNothing(t *testing.T) {
	users := &flakyUsers{}
	e := newEnv(t, withFlakyUsers(users))
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")
	m := post(t, alice, general.ID, "before")

	users.fail.Store(true)
	expect(t, alice.Do(http.MethodPatch, messagePath(general.ID, m.ID), map[string]any{"content": "after"}),
		http.StatusInternalServerError)
	users.fail.Store(false)
	if got := list(t, alice, general.ID, ""); got[0].Content != "before" || got[0].EditedAt != nil {
		t.Fatalf("edited without an event: %+v", got[0])
	}
	if n := len(e.pub.ofType(events.MessageUpdate)); n != 0 {
		t.Fatalf("%d MESSAGE_UPDATE events", n)
	}
}

// A retry with the same nonce (the client lost the response) gets the
// message the first try stored, announced once.
func TestNonceRetryReturnsTheSameMessage(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	var first, again messageBody
	resp := send(alice, general.ID, map[string]any{"content": "hi", "nonce": "n1"})
	expect(t, resp, http.StatusCreated)
	resp.JSON(&first)
	resp = send(alice, general.ID, map[string]any{"content": "hi, edited in the composer", "nonce": "n1"})
	expect(t, resp, http.StatusCreated)
	resp.JSON(&again)

	if again.ID != first.ID || again.Content != "hi" || again.Nonce == nil || *again.Nonce != "n1" {
		t.Fatalf("retry = %+v, first = %+v", again, first)
	}
	if n := e.countMessages(general.ID); n != 1 {
		t.Fatalf("%d messages stored", n)
	}
	if n := len(e.pub.ofType(events.MessageCreate)); n != 1 {
		t.Fatalf("%d MESSAGE_CREATE events", n)
	}
}

// A nonce belongs to one author in one channel.
func TestNonceIsPerAuthorAndChannel(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, bobUser := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bobUser)
	other := createChannel(t, alice, g, "other")

	seen := map[string]bool{}
	for _, s := range []struct {
		name      string
		c         *testkit.Client
		channelID string
	}{
		{"alice in general", alice, general.ID},
		{"alice in other", alice, other.ID},
		{"bob in general", bob, general.ID},
	} {
		m := postNonce(t, s.c, s.channelID, "n1")
		if seen[m.ID] {
			t.Fatalf("%s got a message already sent: %s", s.name, m.ID)
		}
		seen[m.ID] = true
	}
	if n := len(e.pub.ofType(events.MessageCreate)); n != 3 {
		t.Fatalf("%d MESSAGE_CREATE events", n)
	}
}

// After messages.NonceTTL the nonce is free again, and the stale claim is
// dropped.
func TestNonceExpires(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")

	first := postNonce(t, alice, general.ID, "n1")
	if _, err := e.pool.Exec(t.Context(), "UPDATE message_nonces SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if again := postNonce(t, alice, general.ID, "n1"); again.ID == first.ID {
		t.Fatal("an expired nonce still returned the old message")
	}
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_nonces").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d nonce claims kept", n)
	}
}

// Retries racing each other still store one message.
func TestNonceConcurrentRetries(t *testing.T) {
	e := newEnv(t)
	alice, aliceUser := e.user("alice")
	_, general, _ := newGuild(t, alice, "home")
	channelID, userID := parseID(t, general.ID), parseID(t, aliceUser.ID)

	const tries = 8
	nonce := "n1"
	got := make([]snowflake.ID, tries)
	errs := make([]error, tries)
	var wg sync.WaitGroup
	for i := range tries {
		wg.Go(func() {
			m, err := e.msgs.Create(t.Context(), channelID, userID, messages.CreateFields{Content: "hi", Nonce: &nonce})
			got[i], errs[i] = m.ID, err
		})
	}
	wg.Wait()
	for i := range tries {
		if errs[i] != nil {
			t.Fatalf("try %d: %v", i, errs[i])
		}
		if got[i] != got[0] {
			t.Fatalf("tries got different messages: %v", got)
		}
	}
	if n := e.countMessages(general.ID); n != 1 {
		t.Fatalf("%d messages stored", n)
	}
	if n := len(e.pub.ofType(events.MessageCreate)); n != 1 {
		t.Fatalf("%d MESSAGE_CREATE events", n)
	}
}

func postNonce(t *testing.T, c *testkit.Client, channelID, nonce string) messageBody {
	t.Helper()
	resp := send(c, channelID, map[string]any{"content": "hi", "nonce": nonce})
	expect(t, resp, http.StatusCreated)
	var m messageBody
	resp.JSON(&m)
	return m
}
