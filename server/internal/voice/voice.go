package voice

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Limits and intervals.
const (
	// TokenTTL is how long a join token may be used to connect.
	TokenTTL = 10 * time.Minute
	// ReconcileInterval is how often Run rebuilds the states from LiveKit,
	// a safety net for lost webhooks.
	ReconcileInterval = 5 * time.Minute
	// MaxWebhookBytes is the largest webhook body accepted.
	MaxWebhookBytes = 1 << 20
	// WebhookPath is where LiveKit posts its webhooks, under /api/v1.
	WebhookPath = "/livekit/webhook"

	// rpcTimeout bounds one call to the LiveKit server API.
	rpcTimeout = 5 * time.Second
	// A failed removal from LiveKit is tried removeAttempts times in all,
	// waiting removeBackoff, then twice as long each time up to
	// maxRemoveBackoff.
	removeAttempts   = 5
	removeBackoff    = time.Second
	maxRemoveBackoff = time.Minute
	// seenTTL is how long a webhook event ID is remembered, to drop
	// LiveKit's retries of an event already applied.
	seenTTL = 10 * time.Minute
)

// Voice writes per user, shared by joining, the state PATCH and leaving:
// 2 per second, bursts of 10.
var (
	writeRate  = rate.Limit(2)
	writeBurst = 10
	writeTTL   = time.Minute
)

// Errors returned by Service. ToProblem maps them to HTTP problems.
var (
	// ErrUnavailable: voice is not configured, or LiveKit could not do
	// what the request needs.
	ErrUnavailable      = errors.New("voice: unavailable")
	ErrNotVoiceChannel  = errors.New("voice: not a voice channel")
	ErrMissingPerm      = errors.New("voice: missing permission")
	ErrNotInVoice       = errors.New("voice: not in a voice channel")
	ErrInvalidSignature = errors.New("voice: invalid webhook signature")
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
	return "voice: invalid " + strings.Join(names, ", ")
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
	case errors.Is(err, ErrUnavailable):
		return httpx.ServiceUnavailable("voice_unavailable")
	case errors.Is(err, ErrNotVoiceChannel):
		return httpx.BadRequest("not_a_voice_channel", "only voice channels can be joined")
	case errors.Is(err, ErrMissingPerm):
		return httpx.Forbidden("missing_permission")
	case errors.Is(err, ErrNotInVoice):
		return httpx.Conflict("not_in_voice", "the caller is in no voice channel")
	case errors.Is(err, ErrInvalidSignature):
		return httpx.Unauthorized("invalid_signature")
	}
	return channels.ToProblem(err)
}

// State is a user in a voice channel.
type State struct {
	GuildID   snowflake.ID
	ChannelID snowflake.ID
	UserID    snowflake.ID
	// SelfMute and SelfDeaf are what the client says; SelfStream is
	// whether LiveKit has a screen share of the user.
	SelfMute   bool
	SelfDeaf   bool
	SelfStream bool
	// Mute and Deaf are imposed by moderators within this guild.
	Mute     bool
	Deaf     bool
	JoinedAt time.Time
}

// PublicState is the JSON form of a State (schema VoiceState).
type PublicState struct {
	GuildID    snowflake.ID `json:"guild_id"`
	ChannelID  snowflake.ID `json:"channel_id"`
	UserID     snowflake.ID `json:"user_id"`
	SelfMute   bool         `json:"self_mute"`
	SelfDeaf   bool         `json:"self_deaf"`
	SelfStream bool         `json:"self_stream"`
	Mute       bool         `json:"mute"`
	Deaf       bool         `json:"deaf"`
	JoinedAt   string       `json:"joined_at"`
}

// Public returns the JSON form of st.
func (st State) Public() PublicState {
	return PublicState{GuildID: st.GuildID, ChannelID: st.ChannelID, UserID: st.UserID, SelfMute: st.SelfMute,
		SelfDeaf: st.SelfDeaf, SelfStream: st.SelfStream, Mute: st.Mute, Deaf: st.Deaf,
		JoinedAt: httpx.FormatTime(st.JoinedAt)}
}

// UpdatePayload is the payload of VOICE_STATE_UPDATE: the state, and
// whether the user left its channel.
type UpdatePayload struct {
	PublicState
	Left bool `json:"left"`
}

// Channels is what voice needs from the channels module.
type Channels interface {
	Permissions(ctx context.Context, userID, channelID snowflake.ID) (permissions.Permission, snowflake.ID, error)
	Lookup(ctx context.Context, channelID snowflake.ID) (channels.Channel, error)
}

var _ Channels = (*channels.Service)(nil)

// Options configures a Service. Channels is required. Voice is available
// only with Rooms, APIKey and APISecret; otherwise every operation is
// ErrUnavailable.
type Options struct {
	Channels Channels
	// Rooms is the LiveKit server API.
	Rooms RoomClient
	// APIKey and APISecret sign the join tokens and verify the webhooks.
	APIKey    string
	APISecret string
	Publisher events.Publisher // nil: events are dropped
	// Logger is for background failures; nil discards.
	Logger *slog.Logger
	// Now is the clock of the states; nil means time.Now.
	Now func() time.Time
	// WriteLimiter limits the voice writes per user; nil takes the default
	// (2 per second, bursts of 10).
	WriteLimiter *ratelimit.Limiter
}

// session is the state of a user plus what ties it to LiveKit.
type session struct {
	state State
	// room is the LiveKit room name (the channel ID); roomSID and sid
	// identify the room and the participant, so events about older ones
	// are told apart.
	room    string
	roomSID string
	sid     string
	// screens are the SIDs of the screen share tracks published.
	screens map[string]bool
}

// intent is a user's latest join: the channel and the generation its
// token carries. The zero intent means they left: no token may connect.
type intent struct {
	guildID   snowflake.ID
	channelID snowflake.ID
	join      string
}

// removalKey identifies a participant to remove; LiveKit removes by room
// and identity.
type removalKey struct {
	room     string
	identity string
}

// removal is a queued RemoveParticipant: sid is the participant meant,
// attempts the failures so far, due when to try next.
type removal struct {
	sid      string
	attempts int
	due      time.Time
}

// memberKey scopes a removal to the guild the member left.
type memberKey struct {
	guildID snowflake.ID
	userID  snowflake.ID
}

// invalidation rejects admission reads started before a committed removal.
type invalidation struct {
	revision uint64
	at       time.Time
}

// Service implements voice. Create it with NewService and start its
// background work with Run.
type Service struct {
	channels     Channels
	rooms        RoomClient
	key, secret  string
	enabled      bool
	pub          events.Publisher
	log          *slog.Logger
	now          func() time.Time
	writeLimiter *ratelimit.Limiter
	// operations orders media RPCs per user without blocking state reads.
	operations [64]sync.Mutex

	mu         sync.Mutex
	sessions   map[snowflake.ID]*session
	intents    map[snowflake.ID]intent
	moderation map[memberKey]serverFlags
	// rev counts state changes; revs holds the rev of each user's last
	// change, so a reconcile does not undo what happened during it.
	rev             uint64
	revs            map[snowflake.ID]uint64
	seen            map[string]time.Time
	inflight        map[string]chan struct{}
	departed        map[string]time.Time
	membersRemoved  map[memberKey]invalidation
	channelsRemoved map[snowflake.ID]invalidation
	removals        map[removalKey]*removal
	wake            chan struct{}
}

// NewService builds a Service from o. It starts nothing and calls nothing.
func NewService(o Options) *Service {
	s := &Service{channels: o.Channels, rooms: o.Rooms, key: o.APIKey, secret: o.APISecret, pub: o.Publisher,
		log: o.Logger, now: o.Now, writeLimiter: o.WriteLimiter,
		sessions: make(map[snowflake.ID]*session), intents: make(map[snowflake.ID]intent),
		revs: make(map[snowflake.ID]uint64), seen: make(map[string]time.Time),
		removals: make(map[removalKey]*removal), wake: make(chan struct{}, 1)}
	s.inflight = make(map[string]chan struct{})
	s.moderation = make(map[memberKey]serverFlags)
	s.departed = make(map[string]time.Time)
	s.membersRemoved = make(map[memberKey]invalidation)
	s.channelsRemoved = make(map[snowflake.ID]invalidation)
	s.enabled = s.rooms != nil && s.key != "" && s.secret != ""
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.writeLimiter == nil {
		s.writeLimiter = ratelimit.New(writeRate, writeBurst, writeTTL)
	}
	return s
}

// Enabled reports whether voice is configured.
func (s *Service) Enabled() bool { return s.enabled }

// GuildStates returns the states in the voice channels of guildID, by user
// ID. Callers filter them by what the receiver may view.
func (s *Service) GuildStates(guildID snowflake.ID) []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []State{}
	for _, sess := range s.sessions {
		if sess.state.GuildID == guildID {
			out = append(out, sess.state)
		}
	}
	slices.SortFunc(out, func(a, b State) int { return cmp.Compare(a.UserID, b.UserID) })
	return out
}
