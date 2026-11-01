package voice_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/voice"
)

func participant(u userBody, sid string, joined time.Time) voice.Participant {
	return voice.Participant{SID: sid, Identity: u.ID, JoinedAt: joined}
}

func (e *env) reconcile() {
	e.t.Helper()
	if err := e.voice.Reconcile(e.t.Context()); err != nil {
		e.t.Fatalf("Reconcile: %v", err)
	}
}

func TestReconcileRebuildsStatesFromLiveKit(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	joined := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	p := participant(a, "PA_1", joined)
	p.Screens = []string{"TR_screen"}
	e.rooms.put(voice.Room{SID: "RM_1", Name: vc.ID}, p)
	e.rooms.put(voice.Room{SID: "RM_2", Name: "connectivity"}, voice.Participant{SID: "PA_x", Identity: "tester"})

	e.reconcile()
	st, ok := e.stateOf(parseID(t, g.ID), a.id(t))
	if !ok || st.ChannelID != vc.id(t) || !st.JoinedAt.Equal(joined) || !st.SelfStream {
		t.Fatalf("state = %+v (%v), want alice streaming in %s since %v", st, ok, vc.ID, joined)
	}
	e.reconcile()
	if n := len(e.pub.updates()); n != 1 {
		t.Errorf("updates = %d after two reconciles, want 1", n)
	}
	if got := e.rooms.removals(); len(got) != 0 {
		t.Errorf("removals = %v, want none (the connectivity room is not ours)", got)
	}
}

func TestReconcileRemovesParticipantsWhoMayNotConnect(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, b := e.user("bob")
	_, _, vc := newGuild(t, alice, "home") // bob is no member
	e.rooms.put(voice.Room{SID: "RM_1", Name: vc.ID}, participant(b, "PA_1", time.Now()))
	e.reconcile()
	e.run()
	e.rooms.waitRemoved(t, vc.ID+"/"+b.ID)
	if n := len(e.pub.updates()); n != 0 {
		t.Errorf("updates = %d, want none", n)
	}
}

func TestReconcileKeepsTheNewestOfTwoRooms(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, first := newGuild(t, alice, "home")
	second := createVoiceChannel(t, alice, g, "second")
	now := time.Now()
	e.rooms.put(voice.Room{SID: "RM_1", Name: first.ID}, participant(a, "PA_1", now.Add(-time.Minute)))
	e.rooms.put(voice.Room{SID: "RM_2", Name: second.ID}, participant(a, "PA_2", now))
	e.reconcile()
	if st, _ := e.stateOf(parseID(t, g.ID), a.id(t)); st.ChannelID != second.id(t) {
		t.Fatalf("state channel = %v, want the newer %s", st.ChannelID, second.ID)
	}
	e.run()
	e.rooms.waitRemoved(t, first.ID+"/"+a.ID)
}

func TestReconcileDropsStatesWithoutAParticipant(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1") // LiveKit lost it since
	e.reconcile()
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); ok {
		t.Fatal("a state without a participant survived the reconcile")
	}
	ups := e.pub.updates()
	if last := ups[len(ups)-1]; !last.Left {
		t.Fatalf("last update = %+v, want a leave", last)
	}
}

func TestReconcileKeepsStatesOfRoomsItCouldNotList(t *testing.T) {
	e := newEnv(t)
	alice, a := e.user("alice")
	g, _, vc := newGuild(t, alice, "home")
	e.connect(alice, a, vc.ID, "RM_1", "PA_1")
	e.rooms.put(voice.Room{SID: "RM_1", Name: vc.ID}, participant(a, "PA_1", time.Now()))
	e.rooms.failList[vc.ID] = true
	if err := e.voice.Reconcile(t.Context()); !errors.Is(err, errFake) {
		t.Fatalf("Reconcile error = %v, want the partial listing failure", err)
	}
	if _, ok := e.stateOf(parseID(t, g.ID), a.id(t)); !ok {
		t.Fatal("a failed listing cleared the room's state")
	}
}

func TestRemovalIsRetriedAfterAFailure(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user("alice")
	_, b := e.user("bob")
	g, _, vc := newGuild(t, alice, "home")
	e.join(g, b)
	overwrite(t, alice, vc.ID, g.ID, 0, 0, permissions.Connect)
	e.rooms.put(voice.Room{SID: "RM_1", Name: vc.ID}, participant(b, "PA_1", time.Now()))
	e.rooms.mu.Lock()
	e.rooms.failRemovals = 1
	e.rooms.mu.Unlock()
	e.run() // reconciles at once, so bob is queued for removal
	e.rooms.waitRemoved(t, vc.ID+"/"+b.ID)
	if got := e.rooms.removals(); !slices.Equal(got, []string{vc.ID + "/" + b.ID}) {
		t.Fatalf("removals = %v, want one success after the failure", got)
	}
}
