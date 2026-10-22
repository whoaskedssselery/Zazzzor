// Package messages owns the messages of the text channels: history, sending,
// replies, edits, deletions and typing notices.
package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/textx"
)

// Limits.
const (
	// MaxContent is the longest message, in characters (runes) after
	// trimming.
	MaxContent = 4000
	// MaxNonce is the longest nonce, in characters (runes).
	MaxNonce = 64
	// DefaultLimit and MaxLimit bound a page of history.
	DefaultLimit = 50
	MaxLimit     = 100
	// TypingInterval is how often a user's typing in a channel is announced
	// at most.
	TypingInterval = 3 * time.Second
)

// Rate limits per user.
var (
	// Sending: 5 per second, bursts of 10.
	sendRate  = rate.Limit(5)
	sendBurst = 10
	sendTTL   = time.Minute
	// Edits and deletions, shared: 10 per second, bursts of 20.
	writeRate  = rate.Limit(10)
	writeBurst = 20
	writeTTL   = time.Minute
	// Typing: one per TypingInterval per (user, channel).
	typingTTL = time.Minute
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	// ErrNotFound: no such message in the channel (the channel itself being
	// hidden or missing is channels.ErrNotFound).
	ErrNotFound       = errors.New("messages: not found")
	ErrMissingPerm    = errors.New("messages: missing permission")
	ErrNotTextChannel = errors.New("messages: not a text channel")
)

// ValidationError lists the request fields that failed validation.
type ValidationError struct {
	Fields []httpx.FieldError
}

func (e *ValidationError) Error() string {
	names := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		names[i] = f.Field
	}
	return "messages: invalid " + strings.Join(names, ", ")
}

func invalid(field, msg string) error {
	return &ValidationError{Fields: []httpx.FieldError{{Field: field, Message: msg}}}
}

// ToProblem maps the errors of this package (and of channels) to their HTTP
// problem; any other error is returned unchanged.
func ToProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		return httpx.BadRequest("validation_failed", "some fields are invalid", verr.Fields...)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound()
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	case errors.Is(err, ErrNotTextChannel):
		return httpx.BadRequest("not_a_text_channel", "messages can only be sent to text channels")
	}
	return channels.ToProblem(err)
}

// NormalizeContent trims white space from both ends of s and checks the rest
// is 1 to MaxContent characters with something visible and no NUL. Bidi
// controls are allowed: clients render message text escaped.
func NormalizeContent(s string) (string, error) {
	t := strings.TrimFunc(s, unicode.IsSpace)
	switch n := utf8.RuneCountInString(t); {
	case n == 0:
		return "", invalid("content", "must not be empty")
	case n > MaxContent:
		return "", invalid("content", fmt.Sprintf("must be at most %d characters", MaxContent))
	case strings.ContainsRune(t, 0):
		return "", invalid("content", "must not contain NUL characters")
	case !textx.HasVisible(t):
		return "", invalid("content", "must contain a visible character")
	}
	return t, nil
}

// Message is a stored message with its author and the message it replies to.
type Message struct {
	ID        snowflake.ID
	ChannelID snowflake.ID
	GuildID   snowflake.ID
	// Author is nil when the account no longer exists.
	Author    *auth.User
	Content   string
	CreatedAt time.Time
	EditedAt  *time.Time
	ReplyToID *snowflake.ID
	// Referenced is nil when there is no reply or the original is gone.
	Referenced *Stub
	// Nonce is the client's tag of a message just sent; never stored.
	Nonce *string
}

// Stub is the short form of a replied-to message.
type Stub struct {
	ID      snowflake.ID
	Author  *auth.User
	Content string
}

// PublicStub is the JSON form of a Stub (schema MessageStub).
type PublicStub struct {
	ID      snowflake.ID     `json:"id"`
	Author  *auth.PublicUser `json:"author"`
	Content string           `json:"content"`
}

// PublicMessage is the JSON form of a Message (schema Message).
type PublicMessage struct {
	ID                snowflake.ID     `json:"id"`
	ChannelID         snowflake.ID     `json:"channel_id"`
	GuildID           snowflake.ID     `json:"guild_id"`
	Author            *auth.PublicUser `json:"author"`
	Content           string           `json:"content"`
	CreatedAt         string           `json:"created_at"`
	EditedAt          *string          `json:"edited_at"`
	ReplyToID         *snowflake.ID    `json:"reply_to_id"`
	ReferencedMessage *PublicStub      `json:"referenced_message"`
	Nonce             *string          `json:"nonce,omitempty"`
}

func publicUser(u *auth.User) *auth.PublicUser {
	if u == nil {
		return nil
	}
	p := u.Public()
	return &p
}

// Public returns the JSON form of m.
func (m Message) Public() PublicMessage {
	p := PublicMessage{ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, Author: publicUser(m.Author),
		Content: m.Content, CreatedAt: httpx.FormatTime(m.CreatedAt), ReplyToID: m.ReplyToID, Nonce: m.Nonce}
	if m.EditedAt != nil {
		t := httpx.FormatTime(*m.EditedAt)
		p.EditedAt = &t
	}
	if r := m.Referenced; r != nil {
		p.ReferencedMessage = &PublicStub{ID: r.ID, Author: publicUser(r.Author), Content: r.Content}
	}
	return p
}

// DeletePayload is the payload of MESSAGE_DELETE.
type DeletePayload struct {
	ID        snowflake.ID `json:"id"`
	ChannelID snowflake.ID `json:"channel_id"`
	GuildID   snowflake.ID `json:"guild_id"`
}

// TypingPayload is the payload of TYPING_START.
type TypingPayload struct {
	ChannelID snowflake.ID `json:"channel_id"`
	GuildID   snowflake.ID `json:"guild_id"`
	UserID    snowflake.ID `json:"user_id"`
	Timestamp string       `json:"timestamp"`
}

// Channels is what messages needs from the channels module.
type Channels interface {
	Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error)
	Lookup(ctx context.Context, channelID snowflake.ID) (channels.Channel, error)
}

var _ Channels = (*channels.Service)(nil)

// Users is what messages needs from the accounts.
type Users interface {
	UsersByIDs(ctx context.Context, ids []snowflake.ID) ([]auth.User, error)
}

var _ Users = (auth.Accounts)(nil)

// Options configures a Service. Pool, IDs, Channels and Users are required.
type Options struct {
	Pool      *pgxpool.Pool
	IDs       *snowflake.Generator
	Channels  Channels
	Users     Users
	Publisher events.Publisher // nil: events are dropped
	// Now is the clock of typing notices; nil means time.Now.
	Now func() time.Time
	// SendLimiter limits sending per user; nil takes the default (5 per
	// second, bursts of 10).
	SendLimiter *ratelimit.Limiter
	// WriteLimiter limits edits and deletions per user, shared; nil takes
	// the default (10 per second, bursts of 20).
	WriteLimiter *ratelimit.Limiter
	// TypingLimiter throttles typing notices per user and channel; nil
	// takes the default (one per TypingInterval).
	TypingLimiter *ratelimit.Limiter
}

// Service implements messages.
type Service struct {
	pool          *pgxpool.Pool
	q             *store.Queries
	ids           *snowflake.Generator
	channels      Channels
	users         Users
	pub           events.Publisher
	now           func() time.Time
	sendLimiter   *ratelimit.Limiter
	writeLimiter  *ratelimit.Limiter
	typingLimiter *ratelimit.Limiter
}

// NewService builds a Service from o.
func NewService(o Options) *Service {
	s := &Service{pool: o.Pool, q: store.New(), ids: o.IDs, channels: o.Channels, users: o.Users,
		pub: o.Publisher, now: o.Now, sendLimiter: o.SendLimiter, writeLimiter: o.WriteLimiter,
		typingLimiter: o.TypingLimiter}
	if s.now == nil {
		s.now = time.Now
	}
	if s.sendLimiter == nil {
		s.sendLimiter = ratelimit.New(sendRate, sendBurst, sendTTL)
	}
	if s.writeLimiter == nil {
		s.writeLimiter = ratelimit.New(writeRate, writeBurst, writeTTL)
	}
	if s.typingLimiter == nil {
		s.typingLimiter = ratelimit.New(rate.Every(TypingInterval), 1, typingTTL)
	}
	return s
}

func (s *Service) publish(ctx context.Context, e events.Event) {
	if s.pub != nil {
		s.pub.Publish(ctx, e)
	}
}

func channelAudience(guildID, channelID snowflake.ID) events.Audience {
	return events.Audience{Kind: events.AudienceChannel, GuildID: guildID, ChannelID: channelID}
}
