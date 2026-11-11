package messages

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Mount registers the message endpoints on r, which must already run
// auth.Middleware:
//
//	GET, POST /channels/{channel_id}/messages
//	GET, PATCH, DELETE /channels/{channel_id}/messages/{message_id}
//	GET, PUT, DELETE /channels/{channel_id}/messages/{message_id}/reactions/...
//	POST /channels/{channel_id}/typing
func Mount(r chi.Router, s *Service) {
	s.mountAttachments(r)
	if s.files != nil {
		r.Get("/embeds/{embed_image_id}", httpx.Wrap(s.handleEmbedImage))
	}
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, httpx.NoStore)
		sends := r.With(auth.LimitPerUser(s.sendLimiter))
		writes := r.With(auth.LimitPerUser(s.writeLimiter))
		acks := r.With(auth.LimitPerUser(s.ackLimiter))
		acks.Post("/channels/{channel_id}/messages/{message_id}/ack", httpx.Wrap(s.handleAck))
		acks.Post("/guilds/{guild_id}/ack", httpx.Wrap(s.handleAckGuild))
		typing := r.With(auth.LimitPerUser(s.typingCalls))

		s.mountReactions(r)
		r.Get("/channels/{channel_id}/messages", httpx.Wrap(s.handleList))
		sends.Post("/channels/{channel_id}/messages", httpx.Wrap(s.handleCreate))
		writes.Patch("/channels/{channel_id}/messages/{message_id}", httpx.Wrap(s.handleEdit))
		writes.Delete("/channels/{channel_id}/messages/{message_id}", httpx.Wrap(s.handleDelete))
		typing.Post("/channels/{channel_id}/typing", httpx.Wrap(s.handleTyping))
	})
}

func fail(err error) error { return auth.ToProblem(ToProblem(err)) }

func publicMessages(ms []Message) []PublicMessage {
	out := make([]PublicMessage, len(ms))
	for i, m := range ms {
		out[i] = m.Public()
	}
	return out
}

// queryID reads an optional query parameter holding a canonical snowflake
// (positive, written as ID.String() writes it); absent is 0.
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

func parsePage(r *http.Request) (Page, error) {
	var (
		pg  Page
		err error
	)
	if pg.Before, err = queryID(r, "before"); err != nil {
		return Page{}, err
	}
	if pg.After, err = queryID(r, "after"); err != nil {
		return Page{}, err
	}
	pg.Limit, err = queryLimit(r)
	return pg, err
}

// queryLimit parses a canonical page size, with zero meaning absent.
func queryLimit(r *http.Request) (int, error) {
	q := r.URL.Query()
	if !q.Has("limit") {
		return 0, nil
	}
	raw := q.Get("limit")
	limit, err := strconv.Atoi(raw)
	if err != nil || strconv.Itoa(limit) != raw || limit < 1 || limit > MaxLimit {
		return 0, invalid("limit", fmt.Sprintf("must be from 1 to %d", MaxLimit))
	}
	return limit, nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	pg, err := parsePage(r)
	if err != nil {
		return fail(err)
	}
	ms, err := s.List(r.Context(), id, auth.CurrentUser(r).ID, pg)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, publicMessages(ms))
	return nil
}

type createRequest struct {
	Content       *string        `json:"content"`
	ReplyToID     *snowflake.ID  `json:"reply_to_id"`
	Nonce         *string        `json:"nonce"`
	AttachmentIDs []snowflake.ID `json:"attachment_ids"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[createRequest](r)
	if err != nil {
		return err
	}
	m, err := s.Create(r.Context(), id, auth.CurrentUser(r).ID,
		CreateFields{Content: deref(req.Content), ReplyToID: req.ReplyToID, Nonce: req.Nonce, AttachmentIDs: req.AttachmentIDs})
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, m.Public())
	return nil
}

type editRequest struct {
	Content *string `json:"content"`
}

func (s *Service) handleEdit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathID(r, "message_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[editRequest](r)
	if err != nil {
		return err
	}
	m, err := s.Edit(r.Context(), id, msgID, auth.CurrentUser(r).ID, deref(req.Content))
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, m.Public())
	return nil
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathID(r, "message_id")
	if err != nil {
		return err
	}
	if err = s.Delete(r.Context(), id, msgID, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleTyping(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	if err = s.Typing(r.Context(), id, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleAck advances the caller's marker through the path message.
func (s *Service) handleAck(w http.ResponseWriter, r *http.Request) error {
	channelID, err := httpx.PathID(r, "channel_id")
	if err != nil {
		return err
	}
	messageID, err := httpx.PathID(r, "message_id")
	if err != nil {
		return err
	}
	if err = s.Ack(r.Context(), channelID, messageID, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleAckGuild advances the caller's markers in visible guild channels.
func (s *Service) handleAckGuild(w http.ResponseWriter, r *http.Request) error {
	guildID, err := httpx.PathID(r, "guild_id")
	if err != nil {
		return err
	}
	if err = s.AckGuild(r.Context(), guildID, auth.CurrentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
