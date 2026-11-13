package audit

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Mount registers GET /guilds/{guild_id}/audit-logs on r, which must
// already run auth.Middleware.
func Mount(r chi.Router, s *Service) {
	r.With(auth.Require, httpx.NoStore).Get("/guilds/{guild_id}/audit-logs", httpx.Wrap(s.handleList))
}

func invalid(field, msg string) error {
	return httpx.BadRequest("validation_failed", "some fields are invalid", httpx.FieldError{Field: field, Message: msg})
}

// queryID reads an optional canonical snowflake; absent is 0.
func queryID(r *http.Request, name string) (snowflake.ID, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return 0, nil
	}
	raw := q.Get(name)
	id, err := snowflake.Parse(raw)
	if err != nil || id <= 0 || id.String() != raw {
		return 0, invalid(name, "must be a positive snowflake ID")
	}
	return id, nil
}

// queryInt reads an optional canonical integer from lo to hi; absent is 0.
func queryInt(r *http.Request, name string, lo, hi int) (int, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return 0, nil
	}
	raw := q.Get(name)
	n, err := strconv.Atoi(raw)
	if err != nil || strconv.Itoa(n) != raw || n < lo || n > hi {
		return 0, invalid(name, "must be from "+strconv.Itoa(lo)+" to "+strconv.Itoa(hi))
	}
	return n, nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	guildID, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	var f Filter
	if f.Before, err = queryID(r, "before"); err != nil {
		return err
	}
	if f.ActorID, err = queryID(r, "user_id"); err != nil {
		return err
	}
	action, err := queryInt(r, "action_type", 1, 255)
	if err != nil {
		return err
	}
	if f.Action = Action(action); action != 0 && !f.Action.Known() { //nolint:gosec // G115: range checked
		return invalid("action_type", "is not a recorded action")
	}
	if f.Limit, err = queryInt(r, "limit", 1, MaxLimit); err != nil {
		return err
	}
	page, err := s.List(r.Context(), guildID, auth.CurrentUser(r).ID, f)
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound()
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	case errors.Is(err, ErrInvalidFilter):
		return invalid("limit", "is out of range")
	case err != nil:
		return err
	}
	httpx.JSON(w, http.StatusOK, page)
	return nil
}
