package voice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type metricsRoom struct {
	reconcileRooms
	updateError error
}

func (r *metricsRoom) UpdateParticipant(context.Context, string, string, ParticipantPermissions) error {
	return r.updateError
}

// reconcileTimestamp reads the production collector through a real registry.
func reconcileTimestamp(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather voice metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "zazzzor_voice_reconcile_last_success_timestamp_seconds" {
			return family.Metric[0].GetGauge().GetValue()
		}
	}
	t.Fatal("voice reconcile freshness metric missing")
	return 0
}

func TestReconcileMetricAdvancesOnlyAfterCompleteSuccess(t *testing.T) {
	for _, failure := range []string{"room_list", "participant_list", "permissions", "media", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			r := &metricsRoom{reconcileRooms: reconcileRooms{rooms: []Room{{Name: "42", SID: "RM_1"}},
				participants: map[string][]Participant{"42": {{Identity: "1", SID: "PA_1"}}},
				listErrors:   map[string]error{}}}
			cause := errors.New("temporary reconcile failure")
			s := NewService(Options{Rooms: r, Channels: partialChannels{failure: cause}, APIKey: "key", APISecret: "secret", Now: func() time.Time { return now }})
			reg := prometheus.NewRegistry()
			if err := reg.Register(s.Metrics()); err != nil {
				t.Fatalf("register voice metrics: %v", err)
			}
			if got := reconcileTimestamp(t, reg); got != 0 {
				t.Fatalf("initial last success=%v, want 0", got)
			}
			if err := s.Reconcile(t.Context()); err != nil {
				t.Fatalf("successful reconcile: %v", err)
			}
			first := float64(now.Unix())
			now = now.Add(time.Minute)
			ctx := t.Context()
			switch failure {
			case "room_list":
				r.roomsError = cause
			case "participant_list":
				r.listErrors["42"] = cause
			case "permissions":
				r.participants["42"] = append(r.participants["42"], Participant{Identity: "3", SID: "PA_bad"})
			case "media":
				r.updateError = cause
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := s.Reconcile(ctx); err == nil {
				t.Fatal("failed reconcile returned no error")
			}
			if got := reconcileTimestamp(t, reg); got != first {
				t.Fatalf("failed reconcile advanced freshness=%v, want %v", got, first)
			}
			r.roomsError, r.updateError = nil, nil
			delete(r.listErrors, "42")
			r.participants["42"] = []Participant{{Identity: "1", SID: "PA_1"}}
			now = now.Add(time.Minute)
			if err := s.Reconcile(t.Context()); err != nil {
				t.Fatalf("recovered reconcile: %v", err)
			}
			if got := reconcileTimestamp(t, reg); got != float64(now.Unix()) {
				t.Fatalf("recovered freshness=%v, want %d", got, now.Unix())
			}
		})
	}
}
