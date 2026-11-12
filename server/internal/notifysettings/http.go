package notifysettings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Optional is a JSON field that may be absent, null or a value: Set tells
// absent from present, a nil Value is null.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the field was present.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(b, []byte("null")) {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

type overrideBody struct {
	Muted                *bool               `json:"muted"`
	MuteUntil            Optional[time.Time] `json:"mute_until"`
	MessageNotifications *int16              `json:"message_notifications"`
}

type patchBody struct {
	Muted                *bool                   `json:"muted"`
	MuteUntil            Optional[time.Time]     `json:"mute_until"`
	MessageNotifications *int16                  `json:"message_notifications"`
	SuppressEveryone     *bool                   `json:"suppress_everyone"`
	ChannelOverrides     map[string]overrideBody `json:"channel_overrides"`
}

// Mount registers PATCH /users/@me/guilds/{guild_id}/settings on a router
// with auth.Middleware; {guild_id} is a server ID or @me for the DMs.
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		r.Patch("/users/@me/guilds/{guild_id}/settings", httpx.Wrap(s.handlePatch))
	})
}

func (s *Service) handlePatch(w http.ResponseWriter, r *http.Request) error {
	var guildID *snowflake.ID
	if chi.URLParam(r, "guild_id") != "@me" {
		id, err := httpx.PathID(r, "guild_id")
		if err != nil {
			return err
		}
		guildID = &id
	}
	body, err := httpx.Decode[patchBody](r)
	if err != nil {
		return err
	}
	p := Patch{Muted: body.Muted, MuteUntil: body.MuteUntil, MessageNotifications: body.MessageNotifications,
		SuppressEveryone: body.SuppressEveryone}
	if len(body.ChannelOverrides) > MaxOverrides {
		return httpx.BadRequest("too_many_overrides", "at most 500 channel overrides")
	}
	if len(body.ChannelOverrides) > 0 {
		p.ChannelOverrides = make(map[snowflake.ID]OverridePatch, len(body.ChannelOverrides))
		for key, o := range body.ChannelOverrides {
			n, convErr := strconv.ParseInt(key, 10, 64)
			if convErr != nil || n <= 0 {
				return invalid("channel_overrides", "keys must be channel IDs")
			}
			p.ChannelOverrides[snowflake.ID(n)] = OverridePatch{Muted: o.Muted, MuteUntil: o.MuteUntil,
				MessageNotifications: o.MessageNotifications}
		}
	}
	out, err := s.Update(r.Context(), auth.CurrentUser(r).ID, guildID, p)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}
