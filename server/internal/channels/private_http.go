package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
	if (req.Recipient == nil) == (req.Recipients == nil) {
		return fail(invalid("recipient_id", "supply recipient_id or recipient_ids"))
	}
	if req.Recipients != nil {
		var ids []snowflake.ID
		if json.Unmarshal(req.Recipients, &ids) != nil {
			return fail(invalid("recipient_ids", "must be a list of user IDs"))
		}
		group, groupErr := s.CreateGroup(r.Context(), auth.CurrentUser(r).ID, ids)
		if groupErr != nil {
			return fail(groupErr)
		}
		httpx.JSON(w, http.StatusCreated, group)
		return nil
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

func (s *Service) channelKind(ctx context.Context, id snowflake.ID) (Type, error) {
	row, err := s.q.ChannelByID(ctx, s.pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("channels: read channel type: %w", err)
	}
	return Type(row.Type), nil
}

func (s *Service) handlePatchPrivate(w http.ResponseWriter, r *http.Request, id snowflake.ID) error {
	actor := auth.CurrentUser(r).ID
	if _, err := s.PrivateChannel(r.Context(), actor, id); err != nil {
		return fail(err)
	}
	req, err := httpx.Decode[struct {
		Name json.RawMessage `json:"name"`
	}](r)
	if err != nil {
		return err
	}
	if req.Name == nil {
		return fail(invalid("name", "is required"))
	}
	var name *string
	if json.Unmarshal(req.Name, &name) != nil {
		return fail(invalid("name", "must be a string or null"))
	}
	ch, err := s.RenameGroup(r.Context(), id, actor, name)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, ch)
	return nil
}

func (s *Service) handleGroupRecipient(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	target, err := httpx.PathID(r, "user_id")
	if err != nil {
		return err
	}
	actor := auth.CurrentUser(r).ID
	if r.Method == http.MethodPut {
		err = s.AddGroupRecipient(r.Context(), id, actor, target)
	} else {
		err = s.RemoveGroupRecipient(r.Context(), id, actor, target)
	}
	if err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
