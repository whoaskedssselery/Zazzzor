package channels

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Mount registers the channel endpoints on r, which must already run
// auth.Middleware:
//
//	GET, POST, PATCH /guilds/{guild_id}/channels
//	GET, PATCH, DELETE /channels/{channel_id}
//	PUT, DELETE /channels/{channel_id}/permissions/{target_id}
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		writes := r.With(auth.LimitPerUser(s.writeLimiter))

		r.Get("/users/@me/channels", httpx.Wrap(s.handleListPrivate))
		r.Post("/users/@me/channels", httpx.Wrap(s.handleOpenPrivate))

		r.Get("/guilds/{guild_id}/channels", httpx.Wrap(s.handleList))
		writes.Post("/guilds/{guild_id}/channels", httpx.Wrap(s.handleCreate))
		writes.Patch("/guilds/{guild_id}/channels", httpx.Wrap(s.handleReorder))

		r.Get("/channels/{channel_id}", httpx.Wrap(s.handleGet))
		writes.Patch("/channels/{channel_id}", httpx.Wrap(s.handlePatch))
		writes.Delete("/channels/{channel_id}", httpx.Wrap(s.handleDelete))

		writes.Put("/channels/{channel_id}/permissions/{target_id}", httpx.Wrap(s.handlePutOverwrite))
		writes.Delete("/channels/{channel_id}/permissions/{target_id}", httpx.Wrap(s.handleDeleteOverwrite))
	})
}

func fail(err error) error { return auth.ToProblem(ToProblem(err)) }

func publicChannels(cs []Channel) []PublicChannel {
	out := make([]PublicChannel, len(cs))
	for i, c := range cs {
		out[i] = c.Public()
	}
	return out
}

// nullableID reads a raw JSON field that may be absent (nil result), null
// (pointer to nil) or a snowflake.
func nullableID(field string, raw json.RawMessage) (**snowflake.ID, error) {
	if raw == nil {
		return nil, nil
	}
	var p *snowflake.ID
	if !bytes.Equal(raw, []byte("null")) {
		var id snowflake.ID
		if json.Unmarshal(raw, &id) != nil || id <= 0 {
			return nil, invalid(field, "must be a channel ID or null")
		}
		p = &id
	}
	return &p, nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	cs, err := s.VisibleChannels(r.Context(), auth.CurrentUser(r).ID, id)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, publicChannels(cs))
	return nil
}

type createRequest struct {
	Name     *string       `json:"name"`
	Type     *int          `json:"type"`
	ParentID *snowflake.ID `json:"parent_id"`
	Topic    *string       `json:"topic"`
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[createRequest](r)
	if err != nil {
		return err
	}
	switch {
	case req.Name == nil:
		return fail(invalid("name", "is required"))
	case req.Type == nil:
		return fail(invalid("type", "is required"))
	case *req.Type < 0 || *req.Type > 0xFF:
		return fail(invalid("type", "must be 0 (text), 2 (voice) or 4 (category)"))
	}
	c, err := s.CreateChannel(r.Context(), id, auth.CurrentUser(r).ID, CreateFields{
		Name: *req.Name, Type: Type(*req.Type), ParentID: req.ParentID, Topic: req.Topic, //nolint:gosec // G115: range checked
	})
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, c.Public())
	return nil
}

type positionRequest struct {
	ID       snowflake.ID    `json:"id"`
	Position *int            `json:"position"`
	ParentID json.RawMessage `json:"parent_id"`
}

func (s *Service) handleReorder(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[[]positionRequest](r)
	if err != nil {
		return err
	}
	moves := make([]Move, len(req))
	for i, m := range req {
		if m.Position == nil {
			return fail(invalid("channels", "every entry needs a position"))
		}
		parent, perr := nullableID("channels", m.ParentID)
		if perr != nil {
			return fail(perr)
		}
		moves[i] = Move{ID: m.ID, Position: *m.Position, ParentID: parent}
	}
	cs, err := s.ReorderChannels(r.Context(), id, auth.CurrentUser(r).ID, moves)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, publicChannels(cs))
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	c, err := s.Channel(r.Context(), auth.CurrentUser(r).ID, id)
	if err != nil {
		return fail(err)
	}
	if c.Type.Private() {
		dm, dmErr := s.PrivateChannel(r.Context(), auth.CurrentUser(r).ID, id)
		if dmErr != nil {
			return fail(dmErr)
		}
		httpx.JSON(w, http.StatusOK, dm)
		return nil
	}
	httpx.JSON(w, http.StatusOK, c.Public())
	return nil
}

// patchRequest keeps topic and parent_id raw to tell absent from null.
type patchRequest struct {
	Name            *string         `json:"name"`
	Topic           json.RawMessage `json:"topic"`
	ParentID        json.RawMessage `json:"parent_id"`
	SyncPermissions *bool           `json:"sync_permissions"`
}

func (s *Service) handlePatch(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[patchRequest](r)
	if err != nil {
		return err
	}
	f := UpdateFields{Name: req.Name, SyncPermissions: req.SyncPermissions}
	if req.Topic != nil {
		var topic *string
		if !bytes.Equal(req.Topic, []byte("null")) {
			var t string
			if json.Unmarshal(req.Topic, &t) != nil {
				return fail(invalid("topic", "must be a string or null"))
			}
			topic = &t
		}
		f.Topic = &topic
	}
	if f.ParentID, err = nullableID("parent_id", req.ParentID); err != nil {
		return fail(err)
	}
	c, err := s.UpdateChannel(r.Context(), id, auth.CurrentUser(r).ID, f)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, c.Public())
	return nil
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	row, lookupErr := s.Lookup(r.Context(), id)
	if lookupErr != nil {
		return fail(lookupErr)
	}
	if row.Type.Private() {
		if err = s.CloseDirect(r.Context(), id, auth.CurrentUser(r).ID); err != nil {
			return fail(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if err = s.DeleteChannel(r.Context(), id, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type overwriteRequest struct {
	Type  *int                    `json:"type"`
	Allow *permissions.Permission `json:"allow"`
	Deny  *permissions.Permission `json:"deny"`
}

func (s *Service) handlePutOverwrite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	target, err := httpx.PathID(r, "target_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[overwriteRequest](r)
	if err != nil {
		return err
	}
	if req.Type == nil {
		return fail(invalid("type", "is required"))
	}
	f := OverwriteFields{Type: permissions.OverwriteType(*req.Type)}
	if req.Allow != nil {
		f.Allow = *req.Allow
	}
	if req.Deny != nil {
		f.Deny = *req.Deny
	}
	c, err := s.PutOverwrite(r.Context(), id, target, auth.CurrentUser(r).ID, f)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, c.Public())
	return nil
}

func (s *Service) handleDeleteOverwrite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	target, err := httpx.PathID(r, "target_id")
	if err != nil {
		return err
	}
	if err = s.DeleteOverwrite(r.Context(), id, target, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
