package voice_test

import (
	"bytes"
	"net/http"
	"slices"
	"testing"

	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

func TestWebhookAcceptsOnlyValidSignatures(t *testing.T) {
	e := newEnv(t)
	body, err := protojson.Marshal(&livekit.WebhookEvent{Event: "room_started", Id: "EV_sig",
		Room: lkRoom("connectivity", "RM_1")})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	expect(t, e.postHook(body, sign(t, testKey, testSecret, body)), http.StatusNoContent)

	tampered := bytes.Replace(body, []byte("room_started"), []byte("room_finished"), 1)
	for name, tc := range map[string]struct {
		body []byte
		auth string
	}{
		"missing":       {body, ""},
		"tampered body": {tampered, sign(t, testKey, testSecret, body)},
		"wrong secret":  {body, sign(t, testKey, "ffffffffffffffffffffffffffffffff", body)},
		"other key":     {body, sign(t, "APIother", testSecret, body)},
		"not a token":   {body, "Bearer nonsense"},
	} {
		t.Run(name, func(t *testing.T) {
			problemOf(t, e.postHook(tc.body, tc.auth), http.StatusUnauthorized, "invalid_signature")
		})
	}
}

func TestWebhookRefusesOversizedBodies(t *testing.T) {
	e := newEnv(t)
	body := bytes.Repeat([]byte(" "), 1<<20+1)
	problemOf(t, e.postHook(body, sign(t, testKey, testSecret, body)), http.StatusBadRequest, "body_too_large")
}

func TestWebhookJoinCreatesTheState(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")

	st, ok := e.stateOf(parseID(t, g.ID), a.id(t))
	if !ok || st.ChannelID != vc.id(t) || st.SelfMute || st.SelfDeaf || st.SelfStream || st.Mute || st.Deaf ||
		st.JoinedAt.IsZero() {
		t.Fatalf("state = %+v (%v), want alice in %s", st, ok, vc.ID)
	}
	ups := e.pub.updates()
	if len(ups) != 1 || ups[0].Left || ups[0].UserID != a.id(t) || ups[0].ChannelID != vc.id(t) {
		t.Fatalf("updates = %+v, want one join", ups)
	}
	ev := e.pub.events[len(e.pub.events)-1]
	if ev.Audience.ChannelID != vc.id(t) {
		t.Errorf("audience = %+v, want the viewers of the channel", ev.Audience)
	}
}

func TestWebhookRepeatedJoinAnnouncesOnce(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	_, _, vc := newGuild(t, alice, "home")
	join := claims(t, joinVoice(t, alice, vc.ID).Token).Attributes["zazzzor.join"]
	ev := &livekit.WebhookEvent{Event: "participant_joined", Id: "EV_same", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", join)}
	e.hook(ev)
	e.hook(ev) // a retry with the same ID
	ev.Id = ""
	e.hook(ev) // the same participant again under another ID
	if n := len(e.pub.updates()); n != 1 {
		t.Fatalf("updates = %d, want 1", n)
	}
}

func TestWebhookStaleLeaveKeepsTheNewerSession(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	e.connect(alice, a, vc.ID, "RM_1", "PA_2") // reconnected
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", "")})
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); !ok {
		t.Fatal("a late leave of the old participant cleared the new one")
	}
	e.hook(&livekit.WebhookEvent{Event: "participant_left", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_2", "")})
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("the leave of the current participant kept the state")
	}
	ups := e.pub.updates()
	if last := ups[len(ups)-1]; !last.Left || last.ChannelID != vc.id(t) {
		t.Fatalf("last update = %+v, want left %s", last, vc.ID)
	}
}

func TestWebhookJoinWithAnOutdatedTokenIsRemoved(t *testing.T) {
	e := newEnv(t)
	e.run()
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	old := claims(t, joinVoice(t, alice, vc.ID).Token).Attributes["zazzzor.join"]
	joinVoice(t, alice, vc.ID) // a newer join
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(a, "PA_1", old)})
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("an outdated token created a state")
	}
	e.rooms.waitRemoved(t, vc.ID+"/"+a.ID)
}

func TestWebhookJoinWithoutConnectIsRemoved(t *testing.T) {
	e := newEnv(t)
	e.run()
	alice, _ := e.user("alice")
	bob, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	join := claims(t, joinVoice(t, bob, vc.ID).Token).Attributes["zazzzor.join"]
	overwrite(t, alice, vc.ID, g.ID, 0, 0, permissions.Connect) // taken away after the token
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom(vc.ID, "RM_1"),
		Participant: lkParticipant(b, "PA_1", join)})
	if _, ok := e.stateOf(parseID(t, g.ID), b.id(t)); ok {
		t.Fatal("a member without CONNECT got a state")
	}
	e.rooms.waitRemoved(t, vc.ID+"/"+b.ID)
}

func TestWebhookScreenShareSetsSelfStream(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	track := func(event, participant, sid string, src livekit.TrackSource) {
		e.hook(&livekit.WebhookEvent{Event: event, Room: lkRoom(vc.ID, "RM_1"),
			Participant: lkParticipant(a, participant, ""), Track: &livekit.TrackInfo{Sid: sid, Source: src}})
	}
	streaming := func() bool {
		st, _ := e.stateOf(parseID(t, g.ID), a.id(t))
		return st.SelfStream
	}
	track("track_published", "PA_1", "TR_audio", livekit.TrackSource_SCREEN_SHARE_AUDIO)
	if streaming() {
		t.Fatal("screen share audio alone set self_stream")
	}
	track("track_published", "PA_1", "TR_screen", livekit.TrackSource_SCREEN_SHARE)
	if !streaming() {
		t.Fatal("a screen share did not set self_stream")
	}
	track("track_unpublished", "PA_0", "TR_screen", livekit.TrackSource_SCREEN_SHARE)
	if !streaming() {
		t.Fatal("an unpublish of another participant cleared self_stream")
	}
	track("track_unpublished", "PA_1", "TR_screen", livekit.TrackSource_SCREEN_SHARE)
	if streaming() {
		t.Fatal("unpublishing the screen share kept self_stream")
	}
	if n := len(e.pub.updates()); n != 3 {
		t.Errorf("updates = %d, want 3 (join, stream on, stream off)", n)
	}
}

func TestWebhookRoomFinishedClearsOnlyThatRoom(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_2", "PA_1")
	e.hook(&livekit.WebhookEvent{Event: "room_finished", Room: lkRoom(vc.ID, "RM_1")}) // an older room
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); !ok {
		t.Fatal("room_finished of an older room cleared the state")
	}
	e.hook(&livekit.WebhookEvent{Event: "room_finished", Room: lkRoom(vc.ID, "RM_2")})
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("room_finished kept the state")
	}
}

func TestWebhookIgnoresRoomsThatAreNotChannels(t *testing.T) {
	e := newEnv(t)
	e.run()
	_, a := e.user("alice")
	e.hook(&livekit.WebhookEvent{Event: "participant_joined", Room: lkRoom("connectivity", "RM_1"),
		Participant: lkParticipant(a, "PA_1", "")})
	if len(e.pub.updates()) != 0 || len(e.rooms.removals()) != 0 {
		t.Fatalf("updates %v, removals %v, want none", e.pub.updates(), e.rooms.removals())
	}
}

func TestJoiningAnotherChannelLeavesThePreviousOne(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, first := newGuild(t, alice, "home")
	second := createVoiceChannel(t, alice, g, "second")
	e.connect(alice, a, first.ID, "RM_1", "PA_1")

	joinVoice(t, alice, second.ID)
	if got := e.rooms.removals(); !slices.Equal(got, []string{first.ID + "/" + a.ID}) {
		t.Fatalf("removals = %v, want alice from the first room", got)
	}
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("the state in the first channel stayed")
	}
	ups := e.pub.updates()
	if last := ups[len(ups)-1]; !last.Left || last.ChannelID != first.id(t) {
		t.Fatalf("last update = %+v, want left %s", last, first.ID)
	}
}

func TestJoinIsUnavailableWhenThePreviousRoomCannotBeLeft(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, first := newGuild(t, alice, "home")
	second := createVoiceChannel(t, alice, g, "second")
	e.connect(alice, a, first.ID, "RM_1", "PA_1")
	e.rooms.failRemovals = 1
	problemOf(t, alice.Do(http.MethodPost, joinPath(second.ID), nil), http.StatusServiceUnavailable, "voice_unavailable")
	if st, ok := e.stateOf(parseID(t, g.ID), a.id(t)); !ok || st.ChannelID != first.id(t) {
		t.Fatalf("state = %+v (%v), want alice still in the first channel", st, ok)
	}
}

func TestWebhookCameraSetsSelfVideo(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	track := func(event, sid string, src livekit.TrackSource) {
		e.hook(&livekit.WebhookEvent{Event: event, Room: lkRoom(vc.ID, "RM_1"),
			Participant: lkParticipant(a, "PA_1", ""), Track: &livekit.TrackInfo{Sid: sid, Source: src}})
	}
	flags := func() (video, stream bool) {
		st, _ := e.stateOf(parseID(t, g.ID), a.id(t))
		return st.SelfVideo, st.SelfStream
	}
	track("track_published", "TR_cam", livekit.TrackSource_CAMERA)
	if video, stream := flags(); !video || stream {
		t.Fatalf("after a camera: video=%v stream=%v, want video only", video, stream)
	}
	track("track_published", "TR_screen", livekit.TrackSource_SCREEN_SHARE)
	track("track_unpublished", "TR_cam", livekit.TrackSource_CAMERA)
	if video, stream := flags(); video || !stream {
		t.Fatalf("after the camera went: video=%v stream=%v, want stream only", video, stream)
	}
	if n := len(e.pub.updates()); n != 4 {
		t.Errorf("updates = %d, want 4 (join, camera on, stream on, camera off)", n)
	}
}
