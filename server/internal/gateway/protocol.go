// Package gateway is the realtime WebSocket endpoint: it authenticates a
// connection by its session cookie, sends HELLO and READY, answers
// heartbeats, tracks presence, and fans the events of the in-process bus
// out to the connections allowed to see them.
//
// Recipients are resolved from the database when each event is dispatched
// (one dispatcher goroutine, in bus order), never from cached membership or
// permissions: a member who left a guild or lost VIEW_CHANNEL stops getting
// its events with the next event.
package gateway

import (
	"encoding/json"
	"time"

	"github.com/coder/websocket"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

// Opcodes of the frames {op, t?, s?, d}.
const (
	// OpDispatch (server → client) carries an event: t is its type, s the
	// sequence number of the frame on this connection (from 1).
	OpDispatch = 0
	// OpHeartbeat (client → server) keeps the connection alive.
	OpHeartbeat = 1
	// OpHello (server → client) is the first frame.
	OpHello = 10
	// OpHeartbeatAck (server → client) answers a heartbeat.
	OpHeartbeatAck = 11
)

// Close codes and their reasons.
const (
	CloseDecodeError    websocket.StatusCode = 4002
	CloseAuthFailed     websocket.StatusCode = 4004
	CloseOverloaded     websocket.StatusCode = 4008
	CloseSessionTimeout websocket.StatusCode = 4009
	CloseServiceRestart                      = websocket.StatusServiceRestart // 1012
	// closeInternal is used when READY cannot be built (database failure).
	closeInternal = websocket.StatusInternalError // 1011
)

var closeReasons = map[websocket.StatusCode]string{
	CloseDecodeError:    "decode_error",
	CloseAuthFailed:     "authentication_failed",
	CloseOverloaded:     "overloaded",
	CloseSessionTimeout: "session_timeout",
	CloseServiceRestart: "service_restart",
	closeInternal:       "internal_error",
}

// Protocol limits and defaults.
const (
	// HeartbeatInterval is announced in HELLO.
	HeartbeatInterval = 30 * time.Second
	// DefaultHeartbeatTimeout closes a connection that sent no heartbeat
	// for this long (4009). Hidden browser tabs may run timers only once
	// a minute; allow delayed heartbeats without reconnecting.
	DefaultHeartbeatTimeout = 150 * time.Second
	// DefaultPresenceGrace is how long a user stays online after their
	// last connection closed.
	DefaultPresenceGrace = 5 * time.Second
	// DefaultSendBuffer is how many frames may wait for one connection;
	// a connection that falls further behind is closed (4008).
	DefaultSendBuffer = 256
	// DefaultBusBuffer is how many events may wait for the dispatcher.
	DefaultBusBuffer = 1024
	// DefaultMaxConnsPerUser caps the open connections of one user (429
	// too_many_connections past it).
	DefaultMaxConnsPerUser = 10
	// DefaultMaxConns caps all open connections (503 server_busy past it).
	DefaultMaxConns = 500
	// MaxFrameBytes is the largest client frame; a larger one is 4002.
	MaxFrameBytes = 4096
	// MaxMembers caps the members of a GuildState.
	MaxMembers = 1000

	writeTimeout    = 10 * time.Second
	dispatchTimeout = 5 * time.Second
	readyTimeout    = 10 * time.Second
)

// Presence statuses.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
)

// Event types the gateway itself produces.
const (
	EventReady = "READY"
)

// Hello is the payload of HELLO.
type Hello struct {
	HeartbeatIntervalMS int64 `json:"heartbeat_interval_ms"`
}

// Presence is the payload of PRESENCE_UPDATE and an entry of READY.
type Presence struct {
	UserID snowflake.ID `json:"user_id"`
	Status string       `json:"status"`
}

// GuildState is a guild as one member sees it: the payload of GUILD_CREATE
// and GUILD_SYNC and an entry of READY. Channels are only those the member
// may view, and so are the voice and read states. The member list is capped
// at MaxMembers. Presences include every online member.
type GuildState struct {
	Emojis   []guilds.Emoji           `json:"emojis"`
	Guild    guilds.PublicGuild       `json:"guild"`
	Roles    []guilds.PublicRole      `json:"roles"`
	Channels []channels.PublicChannel `json:"channels"`
	Members  []guilds.PublicMember    `json:"members"`
	MyMember guilds.PublicMember      `json:"my_member"`
	// Presences are the guild's online members (the receiver included).
	Presences []Presence `json:"presences"`
	// ReadStates are the member's stored read states in Channels.
	ReadStates []messages.ReadState `json:"read_states"`
	// VoiceStates are the states in the voice channels of Channels.
	VoiceStates []voice.PublicState `json:"voice_states"`
}

// Ready is the payload of READY.
type Ready struct {
	User      auth.PublicUser `json:"user"`
	Guilds    []GuildState    `json:"guilds"`
	Presences []Presence      `json:"presences"`
	// ReadStates are the user's stored read states across all visible channels.
	ReadStates []messages.ReadState `json:"read_states"`
}

// frame is a server frame on the wire.
type frame struct {
	Op int             `json:"op"`
	T  string          `json:"t,omitempty"`
	S  int64           `json:"s,omitempty"`
	D  json.RawMessage `json:"d"`
}

// clientFrame is what a client may send.
type clientFrame struct {
	Op *int            `json:"op"`
	D  json.RawMessage `json:"d"`
}

// outgoing is a frame waiting in a connection's buffer; the sequence number
// is assigned when it is written.
type outgoing struct {
	op int
	t  string
	d  json.RawMessage
}

var nullJSON = json.RawMessage("null")
