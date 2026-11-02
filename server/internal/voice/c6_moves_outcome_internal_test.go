package voice

import (
	"context"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type outcomeChannels struct{}

func (outcomeChannels) Permissions(context.Context, snowflake.ID, snowflake.ID) (permissions.Permission, snowflake.ID, error) {
	return permissions.All, 7, nil
}
func (outcomeChannels) Lookup(_ context.Context, id snowflake.ID) (channels.Channel, error) {
	return channels.Channel{ID: id, GuildID: 7, Type: channels.TypeVoice}, nil
}

type outcomeMembers struct{}

func (outcomeMembers) Subject(_ context.Context, guildID, userID snowflake.ID) (permissions.Subject, error) {
	return permissions.Subject{UserID: userID, GuildID: guildID, OwnerID: 2}, nil
}

type outcomeRooms struct {
	replacementRoom
	during func()
}

func (r *outcomeRooms) UpdateParticipant(context.Context, string, string, ParticipantPermissions) error {
	r.during()
	return nil
}
func (r *outcomeRooms) RemoveParticipant(context.Context, string, string) error {
	r.during()
	return nil
}
func TestC6AppliedRPCReportsSuccessWithoutOverwritingReplacement(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		for _, change := range []string{"replacement", "member_removed", "channel_removed", "left"} {
			t.Run(map[bool]string{false: "flags", true: "disconnect"}[disconnect]+"/"+change, func(t *testing.T) {
				r := &outcomeRooms{}
				s := NewService(Options{Channels: outcomeChannels{}, Members: outcomeMembers{}, Rooms: r, APIKey: "key", APISecret: "0123456789abcdef0123456789abcdef"})
				source := &session{state: State{GuildID: 7, ChannelID: 42, UserID: 1}, sid: "old", room: "42"}
				replacement := &session{state: State{GuildID: 7, ChannelID: 44, UserID: 1}, sid: "new", room: "44"}
				s.sessions[1] = source
				r.during = func() {
					switch change {
					case "replacement":
						s.mu.Lock()
						s.sessions[1] = replacement
						s.mu.Unlock()
					case "member_removed":
						s.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
					case "channel_removed":
						s.RemoveChannel(t.Context(), 42)
					case "left":
						s.mu.Lock()
						s.dropLocked(t.Context(), 1, false)
						s.mu.Unlock()
					}
				}
				on := true
				fields := ModerationFields{Mute: &on}
				if disconnect {
					fields.Move = &MoveRequest{Disconnect: true}
				}
				if err := s.Moderate(t.Context(), 7, 2, 1, fields); err != nil {
					t.Fatalf("Moderate after applied RPC: %v", err)
				}
				if change == "replacement" {
					if s.sessions[1] != replacement {
						t.Fatalf("session = %p, want replacement %p", s.sessions[1], replacement)
					}
					if !replacement.state.Mute || s.refreshes[7] == nil {
						t.Fatalf("replacement mute/refresh = %v/%v, want true/queued", replacement.state.Mute, s.refreshes[7])
					}
				} else if s.sessions[1] != nil {
					t.Fatalf("session = %v, want none", s.sessions[1])
				}
				wantFlags := change == "replacement" || change == "left"
				if got := s.moderation[memberKey{guildID: 7, userID: 1}].mute; got != wantFlags {
					t.Fatalf("stored mute = %v, want %v", got, wantFlags)
				}
			})
		}
	}
}

// TestC6MemberRemovalClearsMoveAfterSourceLeft exercises the move lifetime
// independently of the source state, which participant_left already removed.
func TestC6MemberRemovalClearsMoveAfterSourceLeft(t *testing.T) {
	s := NewService(Options{})
	source := &session{state: State{GuildID: 7, ChannelID: 42, UserID: 1}}
	s.moves[1] = &pendingMove{source: source, channelID: 44}
	s.RemoveMembers(t.Context(), 7, []snowflake.ID{1})
	s.expireMovesLocked(t.Context())
	if len(s.moves) != 0 || len(s.removals) != 0 {
		t.Fatalf("moves/removals = %v/%v, want empty", s.moves, s.removals)
	}
}
