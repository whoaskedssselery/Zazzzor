package channels

import (
	"encoding/json"
	"net/http"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func (s *Service) handleListPrivate(w http.ResponseWriter, r *http.Request) error {
	list, err := s.PrivateChannels(r.Context(), auth.CurrentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, list)
	return nil
}

type openPrivateRequest struct {
	Recipient  json.RawMessage `json:"recipient_id"`
	Recipients json.RawMessage `json:"recipient_ids"`
}

func (s *Service) handleOpenPrivate(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[openPrivateRequest](r)
	if err != nil {
		return err
	}
	if req.Recipient == nil || req.Recipients != nil {
		return fail(invalid("recipient_id", "supply exactly one recipient"))
	}
	var id snowflake.ID
	if json.Unmarshal(req.Recipient, &id) != nil {
		return fail(invalid("recipient_id", "must be a user ID"))
	}
	ch, created, err := s.OpenDirect(r.Context(), auth.CurrentUser(r).ID, id)
	if err != nil {
		return fail(err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, ch)
	return nil
}
