package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
)

func TestStartFilePurgeStopWaitsForTheJob(t *testing.T) {
	disk, err := files.NewDisk(t.TempDir(), files.Options{Retention: time.Hour})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	stop := startFilePurge(t.Context(), disk, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}
