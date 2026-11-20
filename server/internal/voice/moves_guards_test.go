package voice_test

import (
	"errors"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestFlagsRequireParticipantConnect(t *testing.T) {
	t.Parallel()
	e, c, r := moderationEnv(t, permissions.MuteMembers)
	c.target &^= permissions.Connect
	before := len(r.updates)
	on := true
	err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Mute: &on})
	if !errors.Is(err, channels.ErrNotFound) || len(r.updates) != before {
		t.Fatalf("error/updates = %v/%d, want not found/0", err, len(r.updates)-before)
	}
}

func TestHiddenMoveDestinationErrorDependsOnViewer(t *testing.T) {
	t.Parallel()
	for _, actorHidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "target", true: "actor"}[actorHidden], func(t *testing.T) {
			e, c, _ := moderationEnv(t, permissions.MoveMembers)
			want := voice.ErrMissingPerm
			if actorHidden {
				c.actorChannels = map[snowflake.ID]permissions.Permission{44: 0}
				want = channels.ErrNotFound
			} else {
				c.targetChannels = map[snowflake.ID]permissions.Permission{44: 0}
			}
			err := e.voice.Moderate(t.Context(), 7, 2, 1, voice.ModerationFields{Move: destination(44)})
			if !errors.Is(err, want) || lastMove(e) != 0 {
				t.Fatalf("error/move = %v/%v, want %v/none", err, lastMove(e), want)
			}
		})
	}
}
