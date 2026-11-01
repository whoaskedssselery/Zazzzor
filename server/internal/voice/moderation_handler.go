package voice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// optionalBool distinguishes an omitted boolean from an invalid null.
type optionalBool struct{ value *bool }

// UnmarshalJSON rejects explicit nulls and values other than booleans.
func (b *optionalBool) UnmarshalJSON(raw []byte) error {
	if err := json.Unmarshal(raw, &b.value); err != nil {
		return fmt.Errorf("voice: decode moderation flag: %w", err)
	}
	if b.value == nil {
		return errors.New("voice: moderation flag must be a boolean")
	}
	return nil
}

type moderationRequest struct {
	Mute      optionalBool    `json:"mute"`
	Deaf      optionalBool    `json:"deaf"`
	ChannelID optionalChannel `json:"channel_id"`
}

// optionalChannel distinguishes an omitted destination from disconnect.
type optionalChannel struct{ value **snowflake.ID }

// UnmarshalJSON accepts null or a canonical positive channel ID string.
func (c *optionalChannel) UnmarshalJSON(raw []byte) error {
	var text *string
	if err := json.Unmarshal(raw, &text); err != nil {
		return fmt.Errorf("voice: decode destination: %w", err)
	}
	var id *snowflake.ID
	if text != nil {
		parsed, err := snowflake.Parse(*text)
		if err != nil || parsed <= 0 || parsed.String() != *text {
			return errors.New("voice: destination must be a channel ID string or null")
		}
		id = &parsed
	}
	c.value = &id
	return nil
}

func (s *Service) handleModerate(w http.ResponseWriter, r *http.Request) error {
	guildID, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	userID, err := httpx.PathID(r, "user_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[moderationRequest](r)
	if err != nil {
		return err
	}
	if err = s.Moderate(r.Context(), guildID, auth.CurrentUser(r).ID, userID, ModerationFields{Mute: req.Mute.value, Deaf: req.Deaf.value, ChannelID: req.ChannelID.value}); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
