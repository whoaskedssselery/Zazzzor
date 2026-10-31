package voice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"
)

// joinAttribute is the participant attribute holding the join generation:
// an opaque random value that only says which join a token belongs to.
const joinAttribute = "zazzzor.join"

// Room is a LiveKit room as the voice service sees it.
type Room struct {
	SID  string
	Name string
}

// Participant is a LiveKit participant as the voice service sees it.
type Participant struct {
	SID      string
	Identity string
	// JoinedAt is zero when LiveKit did not say.
	JoinedAt time.Time
	// Join is the join generation from the token, "" without one.
	Join string
	// Screens are the SIDs of the screen share tracks it publishes.
	Screens []string
}

// RoomClient is the part of the LiveKit server API the voice service
// calls. LiveKit implements it; tests use a fake.
type RoomClient interface {
	ListRooms(ctx context.Context) ([]Room, error)
	ListParticipants(ctx context.Context, room string) ([]Participant, error)
	// RemoveParticipant disconnects identity from room; a participant or
	// room that is already gone is not an error.
	RemoveParticipant(ctx context.Context, room, identity string) error
	// UpdateParticipant replaces the participant's publishing and listening
	// permissions; an empty Sources list disables publishing entirely.
	UpdateParticipant(ctx context.Context, room, identity string, p ParticipantPermissions) error
}

// ParticipantPermissions is the explicit set of media actions permitted.
// Data publishing and metadata changes are always disabled.
type ParticipantPermissions struct {
	Subscribe bool
	Sources   []livekit.TrackSource
}

// LiveKit is the RoomClient of a LiveKit server.
type LiveKit struct {
	c *lksdk.RoomServiceClient
}

var _ RoomClient = (*LiveKit)(nil)

// NewLiveKit returns the client of the LiveKit server API at url (http or
// https), authenticated with an API key and secret.
func NewLiveKit(url, apiKey, apiSecret string) *LiveKit {
	return &LiveKit{c: lksdk.NewRoomServiceClient(url, apiKey, apiSecret)}
}

// ListRooms implements RoomClient.
func (l *LiveKit) ListRooms(ctx context.Context) ([]Room, error) {
	resp, err := l.c.ListRooms(ctx, &livekit.ListRoomsRequest{})
	if err != nil {
		return nil, fmt.Errorf("voice: list rooms: %w", err)
	}
	out := make([]Room, len(resp.GetRooms()))
	for i, r := range resp.GetRooms() {
		out[i] = Room{SID: r.GetSid(), Name: r.GetName()}
	}
	return out, nil
}

// ListParticipants implements RoomClient.
func (l *LiveKit) ListParticipants(ctx context.Context, room string) ([]Participant, error) {
	resp, err := l.c.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: room})
	if err != nil {
		return nil, fmt.Errorf("voice: list participants: %w", err)
	}
	out := make([]Participant, len(resp.GetParticipants()))
	for i, p := range resp.GetParticipants() {
		out[i] = participantOf(p)
	}
	return out, nil
}

// RemoveParticipant implements RoomClient.
func (l *LiveKit) RemoveParticipant(ctx context.Context, room, identity string) error {
	_, err := l.c.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: room, Identity: identity})
	if te, ok := errors.AsType[twirp.Error](err); ok && te.Code() == twirp.NotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("voice: remove participant: %w", err)
	}
	return nil
}

// UpdateParticipant implements RoomClient. Empty sources must also clear
// CanPublish: LiveKit interprets an empty source list alone as all sources.
func (l *LiveKit) UpdateParticipant(ctx context.Context, room, identity string, p ParticipantPermissions) error {
	_, err := l.c.UpdateParticipant(ctx, &livekit.UpdateParticipantRequest{
		Room: room, Identity: identity,
		Permission: &livekit.ParticipantPermission{
			CanSubscribe: p.Subscribe, CanPublish: len(p.Sources) > 0,
			CanPublishSources: slices.Clone(p.Sources),
			CanPublishData:    false, CanUpdateMetadata: false,
		},
	})
	if err != nil {
		return fmt.Errorf("voice: update participant permissions: %w", err)
	}
	return nil
}

// participantOf converts what LiveKit reports about a participant.
func participantOf(p *livekit.ParticipantInfo) Participant {
	out := Participant{SID: p.GetSid(), Identity: p.GetIdentity(), Join: p.GetAttributes()[joinAttribute]}
	switch {
	case p.GetJoinedAtMs() > 0:
		out.JoinedAt = time.UnixMilli(p.GetJoinedAtMs()).UTC()
	case p.GetJoinedAt() > 0:
		out.JoinedAt = time.Unix(p.GetJoinedAt(), 0).UTC()
	}
	for _, t := range p.GetTracks() {
		if isScreen(t) {
			out.Screens = append(out.Screens, t.GetSid())
		}
	}
	return out
}

// isScreen reports whether t is the video of a screen share; its audio
// does not count.
func isScreen(t *livekit.TrackInfo) bool { return t.GetSource() == livekit.TrackSource_SCREEN_SHARE }
