package voice_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestMain(m *testing.M) { os.Exit(testkit.Main(m)) }

// recorder remembers every event published.
type recorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recorder) Publish(_ context.Context, e events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

// updates returns the VOICE_STATE_UPDATE payloads so far.
func (p *recorder) updates() []voice.UpdatePayload {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []voice.UpdatePayload
	for _, e := range p.events {
		if e.Type == events.VoiceStateUpdate {
			out = append(out, e.Payload.(voice.UpdatePayload))
		}
	}
	return out
}

// fakeRooms is a RoomClient over rooms held in memory. RemoveParticipant
// takes the participant out of its room; failRemovals makes the next
// removals fail.
type fakeRooms struct {
	mu           sync.Mutex
	rooms        []voice.Room
	participants map[string][]voice.Participant
	failList     map[string]bool
	failRemovals int
	removed      []string // "room/identity", in order
}

var errFake = errors.New("fake LiveKit failure")

func newFakeRooms() *fakeRooms {
	return &fakeRooms{participants: map[string][]voice.Participant{}, failList: map[string]bool{}}
}

func (f *fakeRooms) ListRooms(context.Context) ([]voice.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rooms), nil
}

func (f *fakeRooms) ListParticipants(_ context.Context, room string) ([]voice.Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList[room] {
		return nil, errFake
	}
	return slices.Clone(f.participants[room]), nil
}

func (f *fakeRooms) RemoveParticipant(_ context.Context, room, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemovals > 0 {
		f.failRemovals--
		return errFake
	}
	f.removed = append(f.removed, room+"/"+identity)
	f.participants[room] = slices.DeleteFunc(f.participants[room], func(p voice.Participant) bool {
		return p.Identity == identity
	})
	return nil
}

func (f *fakeRooms) removals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

type env struct {
	t      *testing.T
	pool   *pgxpool.Pool
	guilds *guilds.Service
	chans  *channels.Service
	voice  *voice.Service
	rooms  *fakeRooms
	pub    *recorder
	h      http.Handler
	// lk posts the webhooks, as LiveKit: without Origin or cookies.
	lk *testkit.Client
}

func unlimited() *ratelimit.Limiter { return ratelimit.New(rate.Inf, 1, time.Minute) }

// newEnv serves auth, guilds, channels and voice with a fake LiveKit and
// unlimited voice writes; opts may change the voice options.
func newEnv(t *testing.T, opts ...func(*voice.Options)) *env {
	t.Helper()
	e := &env{t: t, pool: testkit.Postgres(t), pub: &recorder{}, rooms: newFakeRooms()}
	ids := snowflake.NewGenerator(0, time.Now)
	authSvc := auth.NewService(auth.Options{Pool: e.pool, IDs: ids, CookieSecure: true,
		RegisterLimiter: unlimited(), LoginLimiter: unlimited()})
	disk, err := files.NewDisk(t.TempDir(), files.Options{})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	e.guilds = guilds.NewService(guilds.Options{Pool: e.pool, IDs: ids, Users: authSvc, Files: disk, Publisher: e.pub,
		Hooks:         guilds.Hooks{Channels: channels.NewBootstrapper(ids, nil), Overwrites: channels.NewCleaner()},
		CreateLimiter: unlimited(), WriteLimiter: unlimited()})
	e.chans = channels.NewService(channels.Options{Pool: e.pool, IDs: ids, Guilds: e.guilds, Publisher: e.pub,
		WriteLimiter: unlimited()})
	o := voice.Options{Channels: e.chans, Rooms: e.rooms, APIKey: testKey, APISecret: testSecret,
		Publisher: e.pub, WriteLimiter: unlimited()}
	for _, opt := range opts {
		opt(&o)
	}
	e.voice = voice.NewService(o)

	r := chi.NewRouter()
	r.Use(httpx.RequireOrigin([]string{testkit.DefaultOrigin}, http.MethodPost+" /api/v1"+voice.WebhookPath))
	r.Route("/api/v1", func(r chi.Router) {
		voice.MountWebhook(r, e.voice)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(authSvc))
			auth.Mount(r, authSvc)
			guilds.Mount(r, e.guilds)
			channels.Mount(r, e.chans)
			voice.Mount(r, e.voice)
		})
	})
	e.h = r
	e.lk = testkit.NewClient(t, e.h)
	e.lk.Origin = ""
	return e
}

// sign returns the Authorization LiveKit sends with body, signed with key
// and secret.
func sign(t *testing.T, key, secret string, body []byte) string {
	t.Helper()
	sum := sha256.Sum256(body)
	tok, err := lkauth.NewAccessToken(key, secret).SetValidFor(time.Minute).
		SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
	if err != nil {
		t.Fatalf("sign webhook: %v", err)
	}
	return tok
}

// postHook posts body to the webhook with the given Authorization.
func (e *env) postHook(body []byte, authorization string) *testkit.Response {
	e.t.Helper()
	opts := []testkit.RequestOption{testkit.WithHeader("Content-Type", "application/webhook+json")}
	if authorization != "" {
		opts = append(opts, testkit.WithHeader("Authorization", authorization))
	}
	return e.lk.Do(http.MethodPost, "/api/v1"+voice.WebhookPath, body, opts...)
}

// hookID numbers the webhook events of a test.
var hookID atomic.Int64

// hook sends a signed webhook event, with a fresh ID unless it has one,
// and expects it accepted.
func (e *env) hook(ev *livekit.WebhookEvent) {
	e.t.Helper()
	if ev.Id == "" {
		ev.Id = "EV_" + strconv.FormatInt(hookID.Add(1), 10)
	}
	body, err := protojson.Marshal(ev)
	if err != nil {
		e.t.Fatalf("encode webhook: %v", err)
	}
	expect(e.t, e.postHook(body, sign(e.t, testKey, testSecret, body)), http.StatusNoContent)
}

// lkRoom is the LiveKit room of a channel, with a SID.
func lkRoom(channelID, sid string) *livekit.Room { return &livekit.Room{Name: channelID, Sid: sid} }

// lkParticipant is a participant of user u with SID sid, connected with a
// token whose join generation is join.
func lkParticipant(u userBody, sid, join string) *livekit.ParticipantInfo {
	return &livekit.ParticipantInfo{Sid: sid, Identity: u.ID, JoinedAtMs: time.Now().UnixMilli(),
		Attributes: map[string]string{"zazzzor.join": join}}
}

// connect gets u a token for the channel and reports, as LiveKit, that
// they connected with it as participant sid of room roomSID.
func (e *env) connect(c *testkit.Client, u userBody, channelID, roomSID, sid string) {
	e.t.Helper()
	join := claims(e.t, joinVoice(e.t, c, channelID).Token).Attributes["zazzzor.join"]
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(channelID, roomSID),
		Participant: lkParticipant(u, sid, join)})
}

// stateOf returns the voice state of userID in guildID, if any.
func (e *env) stateOf(guildID, userID snowflake.ID) (voice.State, bool) {
	for _, st := range e.voice.GuildStates(guildID) {
		if st.UserID == userID {
			return st, true
		}
	}
	return voice.State{}, false
}

type userBody struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (u userBody) id(t *testing.T) snowflake.ID { return parseID(t, u.ID) }

func parseID(t *testing.T, s string) snowflake.ID {
	t.Helper()
	id, err := snowflake.Parse(s)
	if err != nil {
		t.Fatalf("parse ID %q: %v", s, err)
	}
	return id
}

func (e *env) user(name string) (*testkit.Client, userBody) {
	e.t.Helper()
	c := testkit.NewClient(e.t, e.h)
	resp := c.Do(http.MethodPost, "/api/v1/auth/register", map[string]string{"username": name, "password": "password123"})
	expect(e.t, resp, http.StatusCreated)
	var u userBody
	resp.JSON(&u)
	return c, u
}

func (e *env) join(g guildBody, u userBody) {
	e.t.Helper()
	if err := e.guilds.Join(e.t.Context(), parseID(e.t, g.ID), u.id(e.t)); err != nil {
		e.t.Fatalf("join: %v", err)
	}
}

type guildBody struct {
	ID string `json:"id"`
}

type channelBody struct {
	ID   string `json:"id"`
	Type int    `json:"type"`
}

func (c channelBody) id(t *testing.T) snowflake.ID { return parseID(t, c.ID) }

type problem struct {
	Code string `json:"code"`
}

func expect(t *testing.T, resp *testkit.Response, status int) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d: %s", resp.Status, status, resp.Body)
	}
}

func problemOf(t *testing.T, resp *testkit.Response, status int, code string) {
	t.Helper()
	expect(t, resp, status)
	var p problem
	resp.JSON(&p)
	if p.Code != code {
		t.Fatalf("code = %q, want %q", p.Code, code)
	}
}

func perm(p permissions.Permission) string {
	b, _ := p.MarshalJSON()
	return string(b[1 : len(b)-1])
}

// newGuild creates a guild and returns it with its default text and voice
// channels.
func newGuild(t *testing.T, c *testkit.Client, name string) (g guildBody, text, vc channelBody) {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds", map[string]string{"name": name})
	expect(t, resp, http.StatusCreated)
	resp.JSON(&g)
	resp = c.Do(http.MethodGet, "/api/v1/guilds/"+g.ID+"/channels", nil)
	expect(t, resp, http.StatusOK)
	var cs []channelBody
	resp.JSON(&cs)
	for _, ch := range cs {
		switch ch.Type {
		case 0:
			text = ch
		case 2:
			vc = ch
		}
	}
	if text.ID == "" || vc.ID == "" {
		t.Fatal("the new guild lacks a default text or voice channel")
	}
	return g, text, vc
}

func createVoiceChannel(t *testing.T, c *testkit.Client, g guildBody, name string) channelBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": name, "type": 2})
	expect(t, resp, http.StatusCreated)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}

// overwrite sets a channel overwrite for a role (typ 0) or member (typ 1).
func overwrite(t *testing.T, c *testkit.Client, channelID, targetID string, typ int, allow, deny permissions.Permission) {
	t.Helper()
	expect(t, c.Do(http.MethodPut, "/api/v1/channels/"+channelID+"/permissions/"+targetID,
		map[string]any{"type": typ, "allow": perm(allow), "deny": perm(deny)}), http.StatusOK)
}

// grantRole creates a role with p and gives it to the member.
func grantRole(t *testing.T, owner *testkit.Client, g guildBody, member userBody, p permissions.Permission) {
	t.Helper()
	resp := owner.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/roles", map[string]any{"name": "role", "permissions": perm(p)})
	expect(t, resp, http.StatusCreated)
	var role struct {
		ID string `json:"id"`
	}
	resp.JSON(&role)
	expect(t, owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/members/"+member.ID,
		map[string]any{"roles": []string{role.ID}}), http.StatusOK)
}

type joinBody struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

func joinPath(channelID string) string { return "/api/v1/channels/" + channelID + "/voice" }

// joinVoice asks for a token for the channel and returns it.
func joinVoice(t *testing.T, c *testkit.Client, channelID string) joinBody {
	t.Helper()
	resp := c.Do(http.MethodPost, joinPath(channelID), nil)
	expect(t, resp, http.StatusOK)
	var j joinBody
	resp.JSON(&j)
	return j
}

// claims verifies a join token with the test key and secret and returns
// its grants.
func claims(t *testing.T, token string) *lkauth.ClaimGrants {
	t.Helper()
	_, grants := verifyToken(t, token)
	return grants
}

// tokenExpiry verifies a join token and returns when it expires.
func tokenExpiry(t *testing.T, token string) time.Time {
	t.Helper()
	exp, _ := verifyToken(t, token)
	return exp
}

// verifyToken checks a join token was signed with the test key and secret
// and returns when it expires and its grants.
func verifyToken(t *testing.T, token string) (time.Time, *lkauth.ClaimGrants) {
	t.Helper()
	v, err := lkauth.ParseAPIToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if v.APIKey() != testKey {
		t.Fatalf("token API key = %q, want %q", v.APIKey(), testKey)
	}
	reg, grants, err := v.Verify(testSecret)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if reg.ExpiresAt == nil {
		t.Fatal("the token never expires")
	}
	return reg.ExpiresAt.Time, grants
}
