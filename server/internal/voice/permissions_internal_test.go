package voice

import (
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestMediaPermissionsCombineRolesAndModeration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		mask       permissions.Permission
		mute, deaf bool
		sources    []livekit.TrackSource
	}{
		{"speak", permissions.Speak, false, false, []livekit.TrackSource{livekit.TrackSource_MICROPHONE}},
		{"muted", permissions.Speak | permissions.Stream, true, false, []livekit.TrackSource{livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO, livekit.TrackSource_CAMERA}},
		{"deaf", permissions.Speak, false, true, []livekit.TrackSource{livekit.TrackSource_MICROPHONE}},
		{"no_speak", permissions.Stream, false, false, []livekit.TrackSource{livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO, livekit.TrackSource_CAMERA}},
		{"no_stream", permissions.Speak, false, false, []livekit.TrackSource{livekit.TrackSource_MICROPHONE}},
		{"no_publish", 0, false, false, nil},
		{"muted_without_stream", permissions.Speak, true, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaPermissions(tc.mask, tc.mute, tc.deaf)
			if got.Subscribe != !tc.deaf || !slices.Equal(got.Sources, tc.sources) {
				t.Fatalf("media permissions = %+v, want subscribe=%v sources=%v", got, !tc.deaf, tc.sources)
			}
		})
	}
}
