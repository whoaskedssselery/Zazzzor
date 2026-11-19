package messages

import "github.com/prometheus/client_golang/prometheus"

// Metrics counts preview jobs dropped because the bounded queue is full.
func (s *Service) Metrics() prometheus.Collector {
	return prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "zazzzor_messages_preview_jobs_dropped_total",
		Help: "Total link preview jobs dropped because the queue was full.",
	}, func() float64 {
		if s.previews == nil {
			return 0
		}
		s.previews.statsMu.Lock()
		defer s.previews.statsMu.Unlock()
		return float64(s.previews.dropped)
	})
}
