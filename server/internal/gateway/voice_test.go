package gateway_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

const (
	lkKey    = "APIgateway"
	lkSecret = "0123456789abcdef0123456789abcdef"
)

// fakeLiveKit is a LiveKit server API with empty rooms that records the
// participants removed, as "room/identity".
type fakeLiveKit struct {
	mu      sync.Mutex
	removed []string
}

func (f *fakeLiveKit) removals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

func (f *fakeLiveKit) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var resp proto.Message
		switch strings.TrimPrefix(r.URL.Path, "/twirp/livekit.RoomService/") {
		case "ListRooms":
			resp = &livekit.ListRoomsResponse{}
		case "RemoveParticipant":
			var req livekit.RoomParticipantIdentity
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Errorf("RemoveParticipant request: %v", err)
			}
			f.mu.Lock()
			f.removed = append(f.removed, req.GetRoom()+"/"+req.GetIdentity())
			f.mu.Unlock()
			resp = &livekit.RemoveParticipantResponse{}
		default:
			http.NotFound(w, r)
			return
		}
		b, _ := proto.Marshal(resp)
		w.Header().Set("Content-Type", "application/protobuf")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// voiceEnv is the production wiring with voice on, talking to a fake
// LiveKit, and its background work running.
func voiceEnv(t *testing.T) (*env, *fakeLiveKit) {
	t.Helper()
	lk := &fakeLiveKit{}
	e := newEnvFrom(t, map[string]string{
		"ZAZZZOR_LIVEKIT_URL": lk.serve(t),
		"LIVEKIT_API_KEY":     lkKey,
		"LIVEKIT_API_SECRET":  lkSecret,
	}, gateway.Options{}, nil)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(done)
		e.m.Voice.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return e, lk
}

// connectVoice gets the user of c a token for the channel and reports, as
// LiveKit, that they connected with it.
func (e *env) connectVoice(c *testkit.Client, u userBody, channelID string) {
	e.t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/channels/"+channelID+"/voice", nil)
	expect(e.t, resp, http.StatusOK)
	var j struct {
		Token string `json:"token"`
	}
	resp.JSON(&j)
	v, err := lkauth.ParseAPIToken(j.Token)
	if err != nil {
		e.t.Fatalf("parse token: %v", err)
	}
	_, grants, err := v.Verify(lkSecret)
	if err != nil {
		e.t.Fatalf("verify token: %v", err)
	}
	body, err := protojson.Marshal(&livekit.WebhookEvent{Event: "participant_joined", Id: "EV_" + u.ID + channelID,
		Room:        &livekit.Room{Name: channelID, Sid: "RM_" + channelID},
		Participant: &livekit.ParticipantInfo{Sid: "PA_" + u.ID, Identity: u.ID, Attributes: grants.Attributes}})
	if err != nil {
		e.t.Fatalf("encode webhook: %v", err)
	}
	sum := sha256.Sum256(body)
	tok, err := lkauth.NewAccessToken(lkKey, lkSecret).SetValidFor(time.Minute).
		SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
	if err != nil {
		e.t.Fatalf("sign webhook: %v", err)
	}
	hook := testkit.NewClient(e.t, e.h)
	hook.Origin = ""
	expect(e.t, hook.Do(http.MethodPost, "/api/v1"+voice.WebhookPath, body,
		testkit.WithHeader("Content-Type", "application/webhook+json"), testkit.WithHeader("Authorization", tok)),
		http.StatusNoContent)
}

// inVoice reports whether userID has a voice state in the guild.
func (e *env) inVoice(g guildBody, u userBody) bool {
	e.t.Helper()
	return slices.ContainsFunc(e.m.Voice.GuildStates(parseID(e.t, g.ID)), func(st voice.State) bool {
		return st.UserID == u.id(e.t)
	})
}

func voiceChannel(t *testing.T, c *testkit.Client, g guildBody, name string) channelBody {
	t.Helper()
	resp := c.Do(http.MethodPost, "/api/v1/guilds/"+g.ID+"/channels", map[string]any{"name": name, "type": 2})
	expect(t, resp, http.StatusCreated)
	var ch channelBody
	resp.JSON(&ch)
	return ch
}

func TestVoiceStateUpdateReachesOnlyViewers(t *testing.T) {
	e, _ := voiceEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, b)
	e.join(g, cu)
	secret := voiceChannel(t, alice, g, "secret")
	overwrite(t, alice, secret.ID, g.ID, 0, 0, permissions.ViewChannel)
	overwrite(t, alice, secret.ID, b.ID, 1, permissions.ViewChannel, 0)
	acl, _ := connect(t, alice)
	ccl, _ := connect(t, carol)

	e.connectVoice(bob, b, secret.ID)
	var st voiceStateBody
	decode(t, acl.waitFor(events.VoiceStateUpdate).D, &st)
	if st.UserID != b.ID || st.ChannelID != secret.ID || st.Left {
		t.Fatalf("VOICE_STATE_UPDATE = %+v, want bob joining %s", st, secret.ID)
	}
	if got := types(e.untilSentinel(ccl, cu)); slices.Contains(got, events.VoiceStateUpdate) {
		t.Fatalf("carol, who cannot view the channel, got %v", got)
	}
}

func TestGuildStateListsOnlyVisibleVoiceStates(t *testing.T) {
	e, _ := voiceEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	carol, cu := e.user("carol")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, b)
	e.join(g, cu)
	secret := voiceChannel(t, alice, g, "secret")
	overwrite(t, alice, secret.ID, g.ID, 0, 0, permissions.ViewChannel)
	overwrite(t, alice, secret.ID, b.ID, 1, permissions.ViewChannel, 0)
	e.connectVoice(bob, b, secret.ID)

	_, ar := connect(t, alice)
	st, _ := ar.guild(g.ID)
	if len(st.VoiceStates) != 1 || st.VoiceStates[0].UserID != b.ID || st.VoiceStates[0].ChannelID != secret.ID {
		t.Fatalf("alice's voice_states = %+v, want bob in %s", st.VoiceStates, secret.ID)
	}
	_, cr := connect(t, carol)
	if st, _ = cr.guild(g.ID); st.VoiceStates == nil || len(st.VoiceStates) != 0 {
		t.Fatalf("carol's voice_states = %+v, want []", st.VoiceStates)
	}
}

func TestRemovingAMemberTakesOnlyThemOutOfVoice(t *testing.T) {
	cases := map[string]func(t *testing.T, alice, bob *testkit.Client, g guildBody, b userBody){
		"kick": func(t *testing.T, alice, _ *testkit.Client, g guildBody, b userBody) {
			expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/members/"+b.ID, nil), http.StatusNoContent)
		},
		"ban": func(t *testing.T, alice, _ *testkit.Client, g guildBody, b userBody) {
			expect(t, alice.Do(http.MethodPut, "/api/v1/guilds/"+g.ID+"/bans/"+b.ID, map[string]any{}), http.StatusNoContent)
		},
		"leave": func(t *testing.T, _, bob *testkit.Client, g guildBody, _ userBody) {
			expect(t, bob.Do(http.MethodDelete, "/api/v1/users/@me/guilds/"+g.ID, nil), http.StatusNoContent)
		},
	}
	for name, remove := range cases {
		t.Run(name, func(t *testing.T) {
			e, lk := voiceEnv(t)
			alice, a := e.user("alice")
			bob, b := e.user("bob")
			g, _ := newGuild(t, alice, "guild")
			e.join(g, b)
			vc := voiceChannel(t, alice, g, "talk")
			e.connectVoice(alice, a, vc.ID)
			e.connectVoice(bob, b, vc.ID)

			remove(t, alice, bob, g, b)
			if e.inVoice(g, b) || !e.inVoice(g, a) {
				t.Fatalf("after %s: bob in voice %v, alice %v; want only alice", name, e.inVoice(g, b), e.inVoice(g, a))
			}
			eventually(t, "bob's removal from LiveKit", func() bool { return slices.Contains(lk.removals(), vc.ID+"/"+b.ID) })
			if slices.Contains(lk.removals(), vc.ID+"/"+a.ID) {
				t.Fatalf("removals = %v, alice was removed too", lk.removals())
			}
		})
	}
}

func TestDeletingAGuildTakesEveryoneOutOfVoice(t *testing.T) {
	e, lk := voiceEnv(t)
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	g, _ := newGuild(t, alice, "guild")
	e.join(g, b)
	vc := voiceChannel(t, alice, g, "talk")
	e.connectVoice(alice, a, vc.ID)
	e.connectVoice(bob, b, vc.ID)
	expect(t, alice.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID, nil), http.StatusNoContent)
	if e.inVoice(g, a) || e.inVoice(g, b) {
		t.Fatal("a deleted guild kept voice states")
	}
	eventually(t, "both removals", func() bool {
		r := lk.removals()
		return slices.Contains(r, vc.ID+"/"+a.ID) && slices.Contains(r, vc.ID+"/"+b.ID)
	})
}

func TestDeletingAVoiceChannelTakesEveryoneOutOfIt(t *testing.T) {
	e, lk := voiceEnv(t)
	alice, a := e.user("alice")
	g, _ := newGuild(t, alice, "guild")
	vc := voiceChannel(t, alice, g, "talk")
	other := voiceChannel(t, alice, g, "other")
	e.connectVoice(alice, a, vc.ID)
	expect(t, alice.Do(http.MethodDelete, "/api/v1/channels/"+other.ID, nil), http.StatusNoContent)
	if !e.inVoice(g, a) {
		t.Fatal("deleting another channel took alice out of voice")
	}
	expect(t, alice.Do(http.MethodDelete, "/api/v1/channels/"+vc.ID, nil), http.StatusNoContent)
	if e.inVoice(g, a) {
		t.Fatal("deleting the channel kept alice's voice state")
	}
	eventually(t, "alice's removal", func() bool { return slices.Contains(lk.removals(), vc.ID+"/"+a.ID) })
}
