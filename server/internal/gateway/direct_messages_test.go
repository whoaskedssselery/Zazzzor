package gateway_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

const privateChannels = "/api/v1/users/@me/channels"

type privateChannelBody struct {
	ID            string     `json:"id"`
	Type          int        `json:"type"`
	Name          *string    `json:"name"`
	OwnerID       *string    `json:"owner_id"`
	Recipients    []userBody `json:"recipients"`
	LastMessageID *string    `json:"last_message_id"`
}

func dmExpect(t *testing.T, r *testkit.Response, status int) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d: %s", r.Status, status, r.Body)
	}
}

func dmCode(r *testkit.Response) string {
	var problem struct{ Code string }
	r.JSON(&problem)
	return problem.Code
}

func openDM(t *testing.T, c *testkit.Client, other string, status int) privateChannelBody {
	t.Helper()
	r := c.Do(http.MethodPost, privateChannels, map[string]string{"recipient_id": other})
	dmExpect(t, r, status)
	var ch privateChannelBody
	r.JSON(&ch)
	return ch
}

func makeFriends(t *testing.T, a, b *testkit.Client, author, other userBody) {
	t.Helper()
	dmExpect(t, a.Do(http.MethodPost, "/api/v1/users/@me/relationships", map[string]string{"username": other.Username}), http.StatusNoContent)
	dmExpect(t, b.Do(http.MethodPut, "/api/v1/users/@me/relationships/"+author.ID, map[string]int{"type": 1}), http.StatusNoContent)
}

func dmList(t *testing.T, c *testkit.Client) []privateChannelBody {
	t.Helper()
	r := c.Do(http.MethodGet, privateChannels, nil)
	dmExpect(t, r, http.StatusOK)
	var out []privateChannelBody
	r.JSON(&out)
	return out
}

func TestOpenDirectChannelIsUniqueAndPrivate(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	outsider, o := e.user("eve")
	makeFriends(t, alice, bob, a, b)
	first := openDM(t, alice, b.ID, http.StatusCreated)
	second := openDM(t, bob, a.ID, http.StatusOK)
	if first.ID != second.ID || first.Type != 1 || first.OwnerID != nil || first.Name != nil ||
		len(first.Recipients) != 1 || first.Recipients[0].ID != b.ID || len(second.Recipients) != 1 || second.Recipients[0].ID != a.ID {
		t.Fatalf("opened channels = %+v / %+v, want same DM with relative recipients", first, second)
	}
	for _, c := range []*testkit.Client{alice, bob} {
		if got := dmList(t, c); len(got) != 1 || got[0].ID != first.ID {
			t.Fatalf("list = %+v, want %s", got, first.ID)
		}
	}
	if got := dmList(t, outsider); got == nil || len(got) != 0 {
		t.Fatalf("outsider list = %+v, want []", got)
	}
	channel := "/api/v1/channels/" + first.ID
	dmExpect(t, outsider.Do(http.MethodGet, channel, nil), http.StatusNotFound)
	dmExpect(t, alice.Do(http.MethodDelete, channel, nil), http.StatusNoContent)
	if got := dmList(t, alice); len(got) != 0 {
		t.Fatalf("closed channels = %+v, want []", got)
	}
	if got := dmList(t, bob); len(got) != 1 {
		t.Fatalf("peer channels = %+v, want open DM", got)
	}
	openDM(t, alice, b.ID, http.StatusOK)
	if got := dmList(t, alice); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("reopened = %+v", got)
	}
	for _, target := range []string{a.ID, o.ID, "123"} {
		got := alice.Do(http.MethodPost, privateChannels, map[string]string{"recipient_id": target})
		dmExpect(t, got, http.StatusForbidden)
		if dmCode(got) != "cannot_message_user" {
			t.Errorf("denial = %s", got.Body)
		}
	}
}

func TestDirectChannelBlockKeepsHistory(t *testing.T) {
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, _ := newGuild(t, alice, "shared")
	e.join(g, b)
	ch := openDM(t, alice, b.ID, http.StatusCreated)
	path := "/api/v1/channels/" + ch.ID + "/messages"
	dmExpect(t, alice.Do(http.MethodPost, path, map[string]string{"content": "from server mate"}), http.StatusCreated)
	dmExpect(t, bob.Do(http.MethodPut, "/api/v1/users/@me/relationships/"+a.ID, map[string]int{"type": 2}), http.StatusNoContent)
	for _, tc := range []struct {
		c     *testkit.Client
		other string
	}{{alice, b.ID}, {bob, a.ID}} {
		got := tc.c.Do(http.MethodPost, path, map[string]string{"content": "blocked"})
		dmExpect(t, got, http.StatusForbidden)
		if dmCode(got) != "cannot_message_user" {
			t.Errorf("send denial = %s", got.Body)
		}
		dmExpect(t, tc.c.Do(http.MethodGet, path, nil), http.StatusOK)
		got = tc.c.Do(http.MethodPost, privateChannels, map[string]string{"recipient_id": tc.other})
		dmExpect(t, got, http.StatusForbidden)
		if dmCode(got) != "cannot_message_user" {
			t.Errorf("open denial = %s", got.Body)
		}
	}
}
