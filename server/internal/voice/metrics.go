package voice

import "github.com/prometheus/client_golang/prometheus"

// Metrics exports reconcile freshness without making LiveKit a readiness
// dependency. The timestamp is zero until a wholly successful reconcile.
func (s *Service) Metrics() prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "zazzzor_voice_reconcile_last_success_timestamp_seconds",
		Help: "Unix timestamp of the last voice reconcile without room, permission or media errors; zero before success.",
	}, func() float64 {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.lastReconcile.IsZero() {
			return 0
		}
		return float64(s.lastReconcile.Unix())
	})
}
