package voice

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	lkauth "github.com/livekit/protocol/auth"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// grant is what a member with permissions p may do in the room of
// channelID: subscribe unless deafened, publish the microphone with SPEAK
// unless muted, and the screen share (video and audio) with STREAM; never
// data, the camera or
// their own metadata. Every flag is explicit: LiveKit grants everything
// when none is set.
func grant(channelID snowflake.ID, p permissions.Permission, flags serverFlags) *lkauth.VideoGrant {
	media := mediaPermissions(p, flags.mute, flags.deaf)
	g := &lkauth.VideoGrant{RoomJoin: true, Room: channelID.String()}
	g.SetCanSubscribe(media.Subscribe)
	g.SetCanPublishData(false)
	g.SetCanUpdateOwnMetadata(false)
	g.SetCanPublish(len(media.Sources) > 0)
	if len(media.Sources) > 0 {
		g.SetCanPublishSources(media.Sources)
	}
	return g
}

// token signs the join token of user for channelID: their ID is the
// identity, their display name the name, and join the generation attribute.
func (s *Service) token(user auth.User, channelID snowflake.ID, p permissions.Permission, join string, flags serverFlags) (string, error) {
	return s.signToken(user.ID.String(), user.DisplayName, grant(channelID, p, flags), join)
}

// signToken gives primary and screen connections the same lifetime and binding.
func (s *Service) signToken(identity, name string, grant *lkauth.VideoGrant, join string) (string, error) {
	t, err := lkauth.NewAccessToken(s.key, s.secret).
		SetIdentity(identity).
		SetName(name).
		SetValidFor(TokenTTL).
		SetVideoGrant(grant).
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
