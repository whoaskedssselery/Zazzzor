package main

import (
	"context"
	"log/slog"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
)

// startFilePurge purges trash in the background, at once and then every
// files.PurgeInterval, logging every problem. The returned function stops
// the job and waits for it; serve calls it on shutdown.
func startFilePurge(ctx context.Context, disk *files.Disk, logger *slog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		disk.RunPurge(ctx, files.PurgeInterval, func(err error) {
			logger.Warn("purge trash", "err", err.Error())
		})
	}()
	return func() {
		cancel()
		<-done
	}
}
