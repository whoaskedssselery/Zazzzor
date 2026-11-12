package voice

import (
	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

// mediaPermissions combines the channel's grants with server moderation.
// Lifting a mute never grants a microphone without SPEAK; screen sharing
// remains independent of the microphone, and deafening disables listening.
// STREAM covers the camera too, as Discord's «Video» does; server mute and
// deafen leave it alone.
func mediaPermissions(p permissions.Permission, mute, deaf bool) ParticipantPermissions {
	out := ParticipantPermissions{Subscribe: !deaf}
	if p.Has(permissions.Speak) && !mute {
		out.Sources = append(out.Sources, livekit.TrackSource_MICROPHONE)
	}
	if p.Has(permissions.Stream) {
		out.Sources = append(out.Sources, livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO,
			livekit.TrackSource_CAMERA)
	}
	return out
}
