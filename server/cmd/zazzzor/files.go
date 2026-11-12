package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/app"
	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	filesUsage = "usage: zazzzor files reconcile [--strict]"
	// reconcileTimeout bounds a whole reconcile run.
	reconcileTimeout = 10 * time.Minute
)

// startFilePurge purges trash in the background, at once and then every
// files.PurgeInterval, logging every problem. The returned function stops
// the job and waits for it; serve calls it on shutdown.
func startFilePurge(ctx context.Context, disk *files.Disk, logger *slog.Logger) (stop func()) {
	return startBackground(ctx, func(ctx context.Context) {
		disk.RunPurge(ctx, files.PurgeInterval, func(err error) {
			logger.Warn("purge trash", "err", err.Error())
		})
	})
}

// startBackground runs a worker and returns a function that stops and joins it.
func startBackground(ctx context.Context, run func(context.Context)) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// filesCommand runs `zazzzor files reconcile`: it makes the data directory
// match the database (files.Disk.Reconcile), printing every warning and
// failure with its path and then a summary. It exits 1 when an item failed
// or the run could not start, 2 on bad usage. With --strict an image the
// database refers to and the disk lacks (files.ErrMissingImage) fails the
// run too: after a restore that means the files and the database disagree.
func filesCommand(args []string, env func(string) string, stdout, stderr io.Writer) int {
	strict := len(args) == 2 && args[1] == "--strict"
	if len(args) == 0 || args[0] != "reconcile" || (len(args) != 1 && !strict) {
		_, _ = fmt.Fprintln(stderr, filesUsage)
		return 2
	}
	cfg, err := config.Load(env)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "files reconcile: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	// openDatabase also migrates, so the references are read from the
	// schema this binary knows.
	pool, err := openDatabase(ctx, slog.New(slog.NewJSONHandler(stderr, nil)), cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "files reconcile: open database: %v\n", err)
		return 1
	}
	defer pool.Close()
	m, err := app.NewModules(cfg, pool, snowflake.NewGenerator(0, time.Now), nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "files reconcile: %v\n", err)
		return 1
	}
	rep, err := m.Files.Reconcile(ctx, fileReferences{auth: m.Auth, guilds: m.Guilds, messages: m.Messages})
	for _, w := range rep.Warnings {
		_, _ = fmt.Fprintf(stderr, "files reconcile: warning: %v\n", w)
	}
	for _, f := range rep.Failures {
		_, _ = fmt.Fprintf(stderr, "files reconcile: failed: %v\n", f)
	}
	_, _ = fmt.Fprintf(stdout, "files reconcile: restored %d, trashed %d, purged %d, warnings %d, failed %d\n",
		rep.Restored, rep.Trashed, rep.Purged, len(rep.Warnings), len(rep.Failures))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "files reconcile: %v\n", err)
		return 1
	}
	if len(rep.Failures) > 0 {
		return 1
	}
	if strict && slices.ContainsFunc(rep.Warnings, func(w error) bool { return errors.Is(w, files.ErrMissingImage) }) {
		_, _ = fmt.Fprintln(stderr, "files reconcile: --strict: referenced files are missing")
		return 1
	}
	return 0
}

// fileReferences answers files.Reconcile from the modules that own the
// avatar, icon and attachment records.
type fileReferences struct {
	auth     *auth.Service
	guilds   *guilds.Service
	messages *messages.Service
}

var _ files.References = fileReferences{}

// ImageReferences implements files.References.
func (r fileReferences) ImageReferences(ctx context.Context) ([]files.Reference, error) {
	avatars, err := r.auth.AvatarReferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("avatars: %w", err)
	}
	icons, err := r.guilds.IconReferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("icons: %w", err)
	}
	attachments, err := r.messages.AttachmentReferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("attachments: %w", err)
	}
	return append(append(avatars, icons...), attachments...), nil
}

// ImageReferenced implements files.References.
func (r fileReferences) ImageReferenced(ctx context.Context, ref files.Reference) (bool, error) {
	var ok bool
	var err error
	switch ref.Kind {
	case files.KindAvatar:
		ok, err = r.auth.AvatarReferenced(ctx, ref.OwnerID, ref.Hash)
	case files.KindIcon:
		ok, err = r.guilds.IconReferenced(ctx, ref.OwnerID, ref.Hash)
	case files.KindAttachment:
		id, parseErr := snowflake.Parse(ref.Hash)
		if parseErr != nil {
			return false, fmt.Errorf("attachment reference: %w", parseErr)
		}
		ok, err = r.messages.AttachmentReferenced(ctx, ref.OwnerID, id)
	default:
		return false, fmt.Errorf("unknown file kind %q", ref.Kind)
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", ref.Kind, err)
	}
	return ok, nil
}
