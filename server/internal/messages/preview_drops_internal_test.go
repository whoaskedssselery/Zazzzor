package messages

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPreviewDropsAreCountedAndWarningsAreRateLimited(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	now := time.Now()
	s := NewService(Options{Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	s.previews = &previews{jobs: make(chan previewJob, previewQueueSize)}
	reg := prometheus.NewRegistry()
	if err := reg.Register(s.Metrics()); err != nil {
		t.Fatalf("register metrics: %v", err)
	}
	for range previewQueueSize {
		s.enqueuePreviews(Message{}, []string{"https://example.test/"})
	}
	var producers sync.WaitGroup
	var scrapes sync.WaitGroup
	done := make(chan struct{})
	started := make(chan struct{})
	scrapes.Go(func() {
		close(started)
		for {
			select {
			case <-done:
				return
			default:
				if _, err := reg.Gather(); err != nil {
					t.Errorf("concurrent gather: %v", err)
				}
			}
		}
	})
	<-started
	for range 512 {
		producers.Go(func() { s.enqueuePreviews(Message{}, []string{"https://example.test/"}) })
	}
	producers.Wait()
	close(done)
	scrapes.Wait()
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Errorf("overflow warnings = %d, want 1", got)
	}
	now = now.Add(time.Minute)
	s.enqueuePreviews(Message{}, []string{"https://example.test/"})
	if got := strings.Count(logs.String(), "level=WARN"); got != 2 {
		t.Errorf("warnings after interval = %d, want 2", got)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(families) != 1 || families[0].GetName() != "zazzzor_messages_preview_jobs_dropped_total" || families[0].Metric[0].GetCounter().GetValue() != 513 {
		t.Errorf("drop metrics = %v, want counter 513", families)
	}
}
