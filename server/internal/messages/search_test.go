package messages_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

type searchBody struct {
	Messages     []messageBody `json:"messages"`
	TotalResults int           `json:"total_results"`
}

func search(t *testing.T, c *testkit.Client, path string, q url.Values) searchBody {
	t.Helper()
	resp := c.Do(http.MethodGet, path+"?"+q.Encode(), nil)
	expect(t, resp, http.StatusOK)
	var got searchBody
	resp.JSON(&got)
	return got
}

func contents(ms []messageBody) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Content
	}
	return out
}

func TestSearchGuildMessages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bu)
	secret := createChannel(t, alice, g, "secret")
	denyEveryone(t, alice, g, secret.ID, permissions.ViewChannel)
	nohistory := createChannel(t, alice, g, "nohistory")

	post(t, alice, general.ID, "Смотрели новый фильм вчера")
	post(t, bob, general.ID, "Фильмы ужасов не люблю")
	post(t, alice, general.ID, "Пойдём в кино")
	post(t, alice, general.ID, "Fixed the build on Friday")
	post(t, alice, secret.ID, "Секретный фильм")
	post(t, alice, nohistory.ID, "Старый фильм")
	denyEveryone(t, alice, g, nohistory.ID, permissions.ReadMessageHistory)
	path := "/api/v1/guilds/" + g.ID + "/messages/search"

	// Word forms match, newest first; bob sees neither the hidden channel
	// nor the one without history.
	got := search(t, bob, path, url.Values{"content": {"фильм"}})
	if want := []string{"Фильмы ужасов не люблю", "Смотрели новый фильм вчера"}; !slices.Equal(contents(got.Messages), want) ||
		got.TotalResults != 2 {
		t.Fatalf("фильм = %v (%d), want %v", contents(got.Messages), got.TotalResults, want)
	}
	// The owner sees every channel.
	if got := search(t, alice, path, url.Values{"content": {"фильм"}}); got.TotalResults != 4 {
		t.Fatalf("owner фильм = %v", contents(got.Messages))
	}
	// A word start finds longer words; English is stemmed too.
	if got := search(t, bob, path, url.Values{"content": {"кин"}}); !slices.Equal(contents(got.Messages), []string{"Пойдём в кино"}) {
		t.Fatalf("кин = %v", contents(got.Messages))
	}
	if got := search(t, bob, path, url.Values{"content": {"fixing builds"}}); len(got.Messages) != 1 {
		t.Fatalf("fixing builds = %v", contents(got.Messages))
	}
	// Every word must match; query syntax is only a separator.
	if got := search(t, bob, path, url.Values{"content": {"фильм & | !вчера:*"}}); !slices.Equal(contents(got.Messages),
		[]string{"Смотрели новый фильм вчера"}) {
		t.Fatalf("two words = %v", contents(got.Messages))
	}
	// By author alone, and by author and channel.
	if got := search(t, alice, path, url.Values{"author_id": {bu.ID}}); !slices.Equal(contents(got.Messages),
		[]string{"Фильмы ужасов не люблю"}) {
		t.Fatalf("by bob = %v", contents(got.Messages))
	}
	if got := search(t, bob, path, url.Values{"content": {"фильм"}, "channel_id": {secret.ID}}); len(got.Messages) != 0 {
		t.Fatalf("hidden channel by id = %v", contents(got.Messages))
	}
	// Pages: before the last one shown.
	first := search(t, alice, path, url.Values{"content": {"фильм"}, "limit": {"2"}})
	rest := search(t, alice, path, url.Values{"content": {"фильм"}, "limit": {"2"}, "before": {first.Messages[1].ID}})
	if len(first.Messages) != 2 || len(rest.Messages) != 2 || rest.TotalResults != 4 || rest.Messages[0].ID >= first.Messages[1].ID {
		t.Fatalf("pages = %v then %v", contents(first.Messages), contents(rest.Messages))
	}

	problemOf(t, bob.Do(http.MethodGet, path, nil), http.StatusBadRequest, "validation_failed")
	problemOf(t, bob.Do(http.MethodGet, path+"?content=%21%21", nil), http.StatusBadRequest, "validation_failed")
	problemOf(t, bob.Do(http.MethodGet, path+"?content=x&limit=26", nil), http.StatusBadRequest, "validation_failed")
	stranger, _ := e.user("carol")
	problemOf(t, stranger.Do(http.MethodGet, path+"?content=x", nil), http.StatusNotFound, "not_found")
}

func TestSearchOneChannel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, bu := e.user("bob")
	g, general, _ := newGuild(t, alice, "home")
	e.join(g, bu)
	other := createChannel(t, alice, g, "other")
	post(t, alice, general.ID, "чай с лимоном")
	post(t, alice, other.ID, "чай без сахара")

	got := search(t, bob, "/api/v1/channels/"+general.ID+"/messages/search", url.Values{"content": {"чай"}})
	if !slices.Equal(contents(got.Messages), []string{"чай с лимоном"}) {
		t.Fatalf("one channel = %v", contents(got.Messages))
	}
	denyEveryone(t, alice, g, general.ID, permissions.ReadMessageHistory)
	problemOf(t, bob.Do(http.MethodGet, "/api/v1/channels/"+general.ID+"/messages/search?content=x", nil),
		http.StatusForbidden, "missing_permission")
}
