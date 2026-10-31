package voice_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func TestJoinGrantsFollowPermissions(t *testing.T) {
	const (
		mic         = "microphone"
		screen      = "screen_share"
		screenAudio = "screen_share_audio"
	)
	cases := []struct {
		name    string
		deny    permissions.Permission // on @everyone in the channel
		admin   bool                   // bob holds ADMINISTRATOR
		sources []string               // nil: may not publish
	}{
		{name: "connect only", deny: permissions.Speak | permissions.Stream},
		{name: "speak", deny: permissions.Stream, sources: []string{mic}},
		{name: "stream", deny: permissions.Speak, sources: []string{screen, screenAudio}},
		{name: "speak and stream", sources: []string{mic, screen, screenAudio}},
		{name: "administrator", deny: permissions.Connect | permissions.Speak | permissions.Stream, admin: true,
			sources: []string{mic, screen, screenAudio}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			alice, _ := e.user("alice")
			bob, b := e.user("bob")
			g, _, vc := newGuild(t, alice, "home")
			e.join(g, b)
			if tc.admin {
				grantRole(t, alice, g, b, permissions.Administrator)
			}
			if tc.deny != 0 {
				overwrite(t, alice, vc.ID, g.ID, 0, 0, tc.deny)
			}

			gr := claims(t, joinVoice(t, bob, vc.ID).Token)
			if gr.Identity != b.ID || gr.Name != "bob" {
				t.Errorf("identity, name = %q, %q, want %q, bob", gr.Identity, gr.Name, b.ID)
			}
			v := gr.Video
			if v == nil || !v.RoomJoin || v.Room != vc.ID {
				t.Fatalf("video grant = %+v, want room join of %s", v, vc.ID)
			}
			if !v.GetCanSubscribe() || v.GetCanPublishData() || v.GetCanUpdateOwnMetadata() || v.RoomAdmin || v.RoomCreate {
				t.Errorf("grant = %+v, want subscribe only besides publishing", v)
			}
			if got := v.GetCanPublish(); got != (tc.sources != nil) {
				t.Errorf("canPublish = %v, want %v", got, tc.sources != nil)
			}
			if !slices.Equal(v.CanPublishSources, tc.sources) {
				t.Errorf("canPublishSources = %v, want %v", v.CanPublishSources, tc.sources)
			}
			if gr.Attributes["zazzzor.join"] == "" {
				t.Error("the token carries no join generation")
			}
		})
	}
}

func TestJoinTokenExpiresAfterTenMinutes(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	before := time.Now().Truncate(time.Second)
	tok := joinVoice(t, alice, vc.ID).Token
	if exp := tokenExpiry(t, tok); exp.Before(before.Add(voice.TokenTTL)) || exp.After(time.Now().Add(voice.TokenTTL)) {
		t.Errorf("token expires at %v, want %v after issuing", exp, voice.TokenTTL)
	}
}

func TestJoinWithoutConnectIsForbidden(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	overwrite(t, alice, vc.ID, g.ID, 0, 0, permissions.Connect)
	problemOf(t, bob.Do(http.MethodPost, joinPath(vc.ID), nil), http.StatusForbidden, "missing_permission")
}

func TestJoinRefusesTextChannels(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, text, _ := newGuild(t, alice, "home")
	problemOf(t, alice.Do(http.MethodPost, joinPath(text.ID), nil), http.StatusBadRequest, "not_a_voice_channel")
}

func TestJoinHidesChannelsTheCallerCannotView(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, _, _ := newGuild(t, alice, "home")
	e.join(g, b)
	hidden := createVoiceChannel(t, alice, g, "secret")
	overwrite(t, alice, hidden.ID, g.ID, 0, 0, permissions.ViewChannel)
	_, _, foreign := newGuild(t, alice, "elsewhere")

	for name, id := range map[string]string{"hidden": hidden.ID, "other guild": foreign.ID, "missing": "123"} {
		t.Run(name, func(t *testing.T) {
			problemOf(t, bob.Do(http.MethodPost, joinPath(id), nil), http.StatusNotFound, "not_found")
		})
	}
}

func TestJoinSignalingURLFollowsOrigin(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	if got := joinVoice(t, alice, vc.ID).URL; got != "wss://zazzzor.test" {
		t.Errorf("url for https://zazzzor.test = %q, want wss://zazzzor.test", got)
	}
	user := auth.User{ID: a.id(t), DisplayName: "alice"}
	j, err := e.voice.Join(t.Context(), vc.id(t), user, "http://localhost:5173")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if j.URL != "ws://localhost:5173" {
		t.Errorf("url for http://localhost:5173 = %q, want ws://localhost:5173", j.URL)
	}
}

func TestVoiceWithoutLiveKitIsUnavailable(t *testing.T) {
	e := newEnv(t, func(o *voice.Options) { o.Rooms, o.APIKey, o.APISecret = nil, "", "" })
	alice, _ := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	problemOf(t, alice.Do(http.MethodPost, joinPath(vc.ID), nil), http.StatusServiceUnavailable, "voice_unavailable")
}
