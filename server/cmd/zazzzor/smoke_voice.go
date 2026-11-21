package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// voice joins a disposable voice channel with the API-issued token and
// observes its admission and departure on the authenticated gateway.
func (s *smokeSession) voice(ctx context.Context, c *websocket.Conn, ready json.RawMessage, guildID string, timeout time.Duration) (err error) {
	var identity struct{ User struct{ ID string } }
	if json.Unmarshal(ready, &identity) != nil || identity.User.ID == "" {
		return errors.New("READY has no user to check voice with")
	}
	var channel struct{ ID string }
	if err = s.do(ctx, http.MethodPost, "/api/v1/guilds/"+guildID+"/channels",
		map[string]any{"name": "smoke-voice", "type": 2}, http.StatusCreated, &channel); err != nil {
		return fmt.Errorf("create a voice channel: %w", err)
	}
	if channel.ID == "" {
		return errors.New("the API returned no voice channel ID")
	}
	// A token request can reach the server even if reading its answer fails.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), smokeRequestTimeout)
		defer cancel()
		if leaveErr := s.do(cleanup, http.MethodDelete, "/api/v1/users/@me/voice", nil, http.StatusNoContent, nil); leaveErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up the voice connection: %w", leaveErr))
		}
	}()
	var join struct{ URL, Token string }
	if err = s.do(ctx, http.MethodPost, "/api/v1/channels/"+channel.ID+"/voice", nil, http.StatusOK, &join); err != nil {
		return fmt.Errorf("no voice token (voice may be off; check the LiveKit settings): %w", err)
	}
	if join.URL == "" || join.Token == "" {
		return errors.New("the API returned no LiveKit address or voice token")
	}
	room := lksdk.NewRoom(&lksdk.RoomCallback{})
	disconnect := sync.OnceFunc(room.Disconnect)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), smokeRequestTimeout)
		defer cancel()
		err = errors.Join(err, smokeDisconnect(cleanup, disconnect))
	}()
	connect, cancel := context.WithTimeout(ctx, timeout)
	// SDK diagnostics can contain the token in the signaling URL. Keep them
	// private and return only the safe, owner-facing diagnosis.
	connectErr := room.JoinWithContextAndToken(connect, join.URL, join.Token,
		lksdk.WithAutoSubscribe(false), lksdk.WithConnectTimeout(timeout), lksdk.WithLogger(logger.GetDiscardLogger()))
	cancel()
	if connectErr != nil {
		return fmt.Errorf("LiveKit is unreachable or refused the token; check /rtc and LiveKit (connect timeout %v)", timeout)
	}
	if err = smokeVoiceState(ctx, c, guildID, channel.ID, identity.User.ID, false, timeout); err != nil {
		return err
	}
	// Disconnect makes LiveKit send participant_left, so departure also checks
	// the webhook path instead of relying on the API's synchronous DELETE.
	if err = smokeDisconnect(ctx, disconnect); err != nil {
		return err
	}
	return smokeVoiceState(ctx, c, guildID, channel.ID, identity.User.ID, true, timeout)
}

// smokeDisconnect bounds the wait for the SDK's synchronous signaling write
// so HTTP cleanup can still remove the participant and the temporary guild.
func smokeDisconnect(ctx context.Context, disconnect func()) error {
	ctx, cancel := context.WithTimeout(ctx, smokeRequestTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		disconnect()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("leaving LiveKit did not finish in time; cleaning up through the API")
	}
}

// smokeVoiceState ignores other participants and channels while waiting for
// the exact voice transition, including an explicit left flag.
func smokeVoiceState(parent context.Context, c *websocket.Conn, guildID, channelID, userID string, left bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				if left {
					return fmt.Errorf("the voice state did not clear within %v of leaving; check the webhook address and LiveKit keys", timeout)
				}
				return fmt.Errorf("the LiveKit join webhook did not arrive within %v; check the webhook address and LiveKit keys", timeout)
			}
			return errors.New("the gateway closed while waiting for the voice state")
		}
		// Other events share the connection and may have other shapes: a frame
		// that is not the awaited voice state is skipped, never an error.
		var event struct {
			Op int
			T  string
			D  json.RawMessage
		}
		if json.Unmarshal(data, &event) != nil || event.Op != 0 || event.T != "VOICE_STATE_UPDATE" {
			continue
		}
		var state struct {
			GuildID   string `json:"guild_id"`
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Left      *bool  `json:"left"`
		}
		if json.Unmarshal(event.D, &state) != nil {
			continue
		}
		if state.GuildID == guildID && state.ChannelID == channelID && state.UserID == userID &&
			state.Left != nil && *state.Left == left {
			return nil
		}
	}
}
