package voice

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// grant is what a member with permissions p may do in the room of
// channelID: always subscribe; publish the microphone with SPEAK and the
// screen share (video and audio) with STREAM; never data, the camera or
// their own metadata. Every flag is explicit: LiveKit grants everything
// when none is set.
func grant(channelID snowflake.ID, p permissions.Permission) *lkauth.VideoGrant {
	g := &lkauth.VideoGrant{RoomJoin: true, Room: channelID.String()}
	g.SetCanSubscribe(true)
	g.SetCanPublishData(false)
	g.SetCanUpdateOwnMetadata(false)
	var sources []livekit.TrackSource
	if p.Has(permissions.Speak) {
		sources = append(sources, livekit.TrackSource_MICROPHONE)
	}
	if p.Has(permissions.Stream) {
		sources = append(sources, livekit.TrackSource_SCREEN_SHARE, livekit.TrackSource_SCREEN_SHARE_AUDIO)
	}
	g.SetCanPublish(len(sources) > 0)
	if len(sources) > 0 {
		g.SetCanPublishSources(sources)
	}
	return g
}

// token signs the join token of user for channelID: their ID is the
// identity, their display name the name, and join the generation attribute.
func (s *Service) token(user auth.User, channelID snowflake.ID, p permissions.Permission, join string) (string, error) {
	t, err := lkauth.NewAccessToken(s.key, s.secret).
		SetIdentity(user.ID.String()).
		SetName(user.DisplayName).
		SetValidFor(TokenTTL).
		SetVideoGrant(grant(channelID, p)).
		SetAttributes(map[string]string{joinAttribute: join}).
		ToJWT()
	if err != nil {
		return "", fmt.Errorf("voice: sign token: %w", err)
	}
	return t, nil
}

// newJoin returns a fresh join generation: random, so it says nothing to
// the other participants who can read it.
func newJoin() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails
	return hex.EncodeToString(b[:])
}

// signalingURL turns the request's Origin, already checked against the
// allowed origins, into the LiveKit signaling URL: LiveKit is served on the
// same host and port, by WebSocket.
func signalingURL(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("voice: origin %q is not a URL", origin)
	}
	switch u.Scheme {
	case "https":
		return "wss://" + u.Host, nil
	case "http":
		return "ws://" + u.Host, nil
	}
	return "", fmt.Errorf("voice: origin scheme %q is not http or https", u.Scheme)
}
