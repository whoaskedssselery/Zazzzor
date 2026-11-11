package messages

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	reactionRate  = rate.Limit(4)
	reactionBurst = 8
	reactionTTL   = time.Minute
)

// mountReactions registers reaction routes and the individual message read.
func (s *Service) mountReactions(r chi.Router) {
	const message = "/channels/{channel_id}/messages/{message_id}"
	const reaction = message + "/reactions/{emoji}"
	r.Get(message, httpx.Wrap(s.handleGet))
	r.Get(reaction, httpx.Wrap(s.handleReactionUsers))
	writes := r.With(auth.LimitPerUser(s.reactionLimiter))
	writes.Put(reaction+"/@me", httpx.Wrap(s.handleReactionWrite))
	writes.Delete(reaction+"/@me", httpx.Wrap(s.handleReactionWrite))
	writes.Delete(reaction+"/{user_id}", httpx.Wrap(s.handleReactionWrite))
}

// messagePathIDs parses the IDs shared by message and reaction paths.
func messagePathIDs(r *http.Request) (channelID, messageID snowflake.ID, err error) {
	channelID, err = httpx.PathID(r, "channel_id")
	if err != nil {
		return 0, 0, err
	}
	messageID, err = httpx.PathID(r, "message_id")
	return channelID, messageID, err
}

// reactionEmoji reads the segment exactly once. Chi routes on RawPath when
// it exists, so only in that case does its parameter still need decoding.
func reactionEmoji(r *http.Request) (string, error) {
	emoji := chi.URLParam(r, "emoji")
	if r.URL.RawPath != "" {
		var err error
		emoji, err = url.PathUnescape(emoji)
		if err != nil {
			return "", badReactionEmoji()
		}
	}
	return emoji, nil
}

// handleGet returns one message with the caller's reaction summary.
func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	channelID, messageID, err := messagePathIDs(r)
	if err != nil {
		return err
	}
	m, err := s.Get(r.Context(), channelID, messageID, auth.CurrentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, m.Public())
	return nil
}

// handleReactionUsers returns one page of users who reacted with the emoji.
func (s *Service) handleReactionUsers(w http.ResponseWriter, r *http.Request) error {
	channelID, messageID, err := messagePathIDs(r)
	if err != nil {
		return err
	}
	emoji, err := reactionEmoji(r)
	if err != nil {
		return fail(err)
	}
	after, err := queryID(r, "after")
	if err != nil {
		return fail(err)
	}
	limit, err := queryLimit(r)
	if err != nil {
		return fail(err)
	}
	users, err := s.ReactionUsers(r.Context(), channelID, messageID, auth.CurrentUser(r).ID, emoji, after, limit)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, users)
	return nil
}

// handleReactionWrite adds or removes a reaction for the user in the path.
func (s *Service) handleReactionWrite(w http.ResponseWriter, r *http.Request) error {
	channelID, messageID, err := messagePathIDs(r)
	if err != nil {
		return err
	}
	emoji, err := reactionEmoji(r)
	if err != nil {
		return fail(err)
	}
	var target *snowflake.ID
	if chi.URLParam(r, "user_id") != "" {
		id, err := httpx.PathID(r, "user_id")
		if err != nil {
			return err
		}
		target = &id
	}
	if err := s.changeReaction(r.Context(), channelID, messageID, auth.CurrentUser(r).ID, emoji, r.Method == http.MethodPut, target); err != nil {
		return fail(guilds.ToProblem(err))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
