package voice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
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
	Mute optionalBool `json:"mute"`
	Deaf optionalBool `json:"deaf"`
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
	if err = s.Moderate(r.Context(), guildID, auth.CurrentUser(r).ID, userID, ModerationFields{Mute: req.Mute.value, Deaf: req.Deaf.value}); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
