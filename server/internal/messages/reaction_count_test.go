package messages_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

// A reaction event says how many reacted with its emoji after the change, so
// that a client applying it twice (a reconnect's catch-up) does not count it twice.
func TestReactionEventsCarryTheCountAfterTheChange(t *testing.T) {
	t.Parallel()
	e, _, alice, bob, _, m := reactionFixture(t)
	path := reactionPath(m, thumb) + "/@me"
	expect(t, alice.Do(http.MethodPut, path, nil), http.StatusNoContent)
	expect(t, bob.Do(http.MethodPut, path, nil), http.StatusNoContent)
	expect(t, bob.Do(http.MethodPut, path, nil), http.StatusNoContent) // already there: no event
	expect(t, bob.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	expect(t, alice.Do(http.MethodDelete, path, nil), http.StatusNoContent)

	counts := func(typ string) []int64 {
		var got []int64
		for _, ev := range e.pub.ofType(typ) {
			got = append(got, ev.Payload.(messages.ReactionPayload).Count)
		}
		return got
	}
	if got := counts(events.MessageReactionAdd); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("add counts = %v, want [1 2]", got)
	}
	if got := counts(events.MessageReactionRemove); len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Errorf("remove counts = %v, want [1 0]", got)
	}
}
