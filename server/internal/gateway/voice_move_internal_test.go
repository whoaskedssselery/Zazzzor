package gateway

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type moveViewers struct {
	channels.Resolver
	ids []snowflake.ID
	err error
}

func (v *moveViewers) Viewers(context.Context, snowflake.ID) ([]snowflake.ID, error) {
	return slices.Clone(v.ids), v.err
}

func TestVoiceMoveRechecksDestinationViewers(t *testing.T) {
	v := &moveViewers{ids: []snowflake.ID{1, 2}}
	g := &Gateway{opts: Options{Channels: v}}
	e := events.Event{Type: events.VoiceMove, Audience: events.Audience{Kind: events.AudienceUsers, ChannelID: 44, UserIDs: []snowflake.ID{1}}}
	ids, err := g.recipients(t.Context(), e)
	if err != nil || !slices.Equal(ids, []snowflake.ID{1}) {
		t.Fatalf("move recipients=%v (%v), want only target", ids, err)
	}
	v.ids = []snowflake.ID{2}
	ids, err = g.recipients(t.Context(), e)
	if err != nil || len(ids) != 0 {
		t.Fatalf("revoked destination recipients=%v (%v), want none", ids, err)
	}
	v.err = channels.ErrNotFound
	ids, err = g.recipients(t.Context(), e)
	if err != nil || len(ids) != 0 {
		t.Fatalf("deleted destination recipients=%v (%v), want none", ids, err)
	}
	v.err = errors.New("unavailable viewers")
	if _, err = g.recipients(t.Context(), e); err == nil {
		t.Fatal("failed privacy check did not return an error")
	}
}
