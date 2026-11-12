package messages

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Search limits.
const (
	// SearchLimit is the default and largest page of results.
	SearchLimit = 25
	// SearchQueryMax is the longest search text, in characters.
	SearchQueryMax = 200
	// searchTerms is how many words of the text count.
	searchTerms = 10
	// SearchCountCap is the most results counted; more read as "SearchCountCap+".
	SearchCountCap = 1000
)

// SearchFilter narrows a search. At least one of Content and AuthorID is
// needed.
type SearchFilter struct {
	// Content is the words to find, each also as the start of a longer
	// word ("кин" finds "кино"); word forms match ("фильмы" finds "фильм").
	Content string
	// AuthorID, if set, keeps the messages of that user.
	AuthorID snowflake.ID
	// ChannelID, if set, keeps one channel of the guild.
	ChannelID snowflake.ID
	// Before, if set, gives the results older than that message.
	Before snowflake.ID
	// Limit 0 takes SearchLimit.
	Limit int
}

// SearchResult is a page of results, newest first, and how many there are
// in all (at most SearchCountCap).
type SearchResult struct {
	Messages []Message
	Total    int
}

// SearchGuild finds messages in the text channels of guildID whose history
// userID may read.
func (s *Service) SearchGuild(ctx context.Context, userID, guildID snowflake.ID, f SearchFilter) (SearchResult, error) {
	ids, err := s.channels.SearchableChannels(ctx, userID, guildID)
	if err != nil {
		return SearchResult{}, err
	}
	if f.ChannelID != 0 {
		if !slices.Contains(ids, f.ChannelID) {
			return SearchResult{Messages: []Message{}}, nil
		}
		ids = []snowflake.ID{f.ChannelID}
	}
	return s.search(ctx, userID, guildID, ids, f)
}

// SearchChannel finds messages in one channel (a direct message or group
// too); userID needs READ_MESSAGE_HISTORY there.
func (s *Service) SearchChannel(ctx context.Context, userID, channelID snowflake.ID, f SearchFilter) (SearchResult, error) {
	_, guildID, err := s.access(ctx, userID, channelID, permissions.ReadMessageHistory)
	if err != nil {
		return SearchResult{}, err
	}
	if err := s.requireText(ctx, channelID); err != nil {
		return SearchResult{}, err
	}
	f.ChannelID = 0
	return s.search(ctx, userID, guildID, []snowflake.ID{channelID}, f)
}

func (s *Service) search(ctx context.Context, userID, guildID snowflake.ID, channelIDs []snowflake.ID, f SearchFilter) (SearchResult, error) {
	query, err := searchQuery(f.Content)
	if err != nil {
		return SearchResult{}, err
	}
	if query == "" && f.AuthorID == 0 {
		return SearchResult{}, invalid("content", "must have a word to find, or give author_id")
	}
	lim := f.Limit
	if lim == 0 {
		lim = SearchLimit
	}
	if lim < 1 || lim > SearchLimit {
		return SearchResult{}, invalid("limit", fmt.Sprintf("must be from 1 to %d", SearchLimit))
	}
	if len(channelIDs) == 0 {
		return SearchResult{Messages: []Message{}}, nil
	}
	chans := make([]int64, len(channelIDs))
	for i, id := range channelIDs {
		chans[i] = int64(id)
	}
	rows, err := s.q.SearchMessages(ctx, s.pool, store.SearchMessagesParams{ChannelIds: chans, Query: query,
		AuthorID: int64(f.AuthorID), Before: int64(f.Before), Lim: int32(lim)}) //nolint:gosec // G115: range checked
	if err != nil {
		return SearchResult{}, fmt.Errorf("messages: search: %w", err)
	}
	total, err := s.q.CountSearchMessages(ctx, s.pool, store.CountSearchMessagesParams{ChannelIds: chans,
		Query: query, AuthorID: int64(f.AuthorID), Cap: SearchCountCap})
	if err != nil {
		return SearchResult{}, fmt.Errorf("messages: count search: %w", err)
	}
	// complete works a channel at a time; the order is kept by ID.
	byChannel := map[snowflake.ID][]store.Message{}
	var order []snowflake.ID
	for _, r := range rows {
		if _, ok := byChannel[r.ChannelID]; !ok {
			order = append(order, r.ChannelID)
		}
		byChannel[r.ChannelID] = append(byChannel[r.ChannelID], r)
	}
	out := make([]Message, 0, len(rows))
	for _, ch := range order {
		ms, err := s.complete(ctx, guildID, ch, byChannel[ch], userID)
		if err != nil {
			return SearchResult{}, err
		}
		out = append(out, ms...)
	}
	slices.SortFunc(out, func(a, b Message) int {
		switch {
		case a.ID > b.ID:
			return -1
		case a.ID < b.ID:
			return 1
		}
		return 0
	})
	return SearchResult{Messages: out, Total: int(total)}, nil
}

// searchQuery turns what the user typed into a to_tsquery: every word (a
// run of letters and digits) must match, as a whole word form or as the
// start of one. Other characters only separate words, so nothing typed can
// break the query syntax.
func searchQuery(text string) (string, error) {
	if utf8.RuneCountInString(text) > SearchQueryMax {
		return "", invalid("content", fmt.Sprintf("must be at most %d characters", SearchQueryMax))
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > searchTerms {
		words = words[:searchTerms]
	}
	for i, w := range words {
		words[i] = w + ":*"
	}
	return strings.Join(words, " & "), nil
}
