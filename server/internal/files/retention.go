package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// Trash and its upkeep. A backup dumps the database first and archives the
// data directory after, so an image deleted in between must still be in the
// archive: Delete moves it to trash instead of removing it, and only Purge
// removes it, once it has been in trash longer than the retention (which is
// longer than the interval between backups). After a restore, Reconcile
// moves what the restored database refers to back out of trash.
const (
	// ReconcileGrace protects uploads in flight: an upload writes its file
	// before the database row that refers to it, so Reconcile never moves a
	// live file to trash that was modified less than this long before the
	// run started.
	ReconcileGrace = 1 * time.Hour
	// PurgeInterval is how often the API purges trash.
	PurgeInterval = time.Hour
)

const trashDir = "trash"

// Problems found by Reconcile and Purge. Each is reported for one entry
// (see ItemError) and never stops the rest of the run.
var (
	// ErrMissingImage: the database refers to an image that is neither live
	// nor in trash.
	ErrMissingImage = errors.New("files: referenced image is missing")
	// ErrUnexpectedEntry: an entry this package never writes; it is left
	// where it is.
	ErrUnexpectedEntry = errors.New("files: unexpected entry left in place")
)

// ItemError is a problem with one entry of the data directory.
type ItemError struct {
	Path string // relative to the data directory, with forward slashes
	Err  error
}

func (e *ItemError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *ItemError) Unwrap() error { return e.Err }

// Reference identifies a stored file that the database refers to.
type Reference struct {
	Kind    Kind
	OwnerID snowflake.ID
	Hash    string // image hash or canonical decimal attachment ID
}

func (r Reference) valid() bool {
	if r.Kind == KindAttachment {
		id, err := snowflake.Parse(r.Hash)
		return err == nil && r.OwnerID > 0 && id > 0 && id.String() == r.Hash
	}
	return r.Kind.valid() && r.OwnerID > 0 && ValidHash(r.Hash)
}

// References tells Reconcile which images the database refers to. The
// composition root implements it over the modules that own those tables.
type References interface {
	// ImageReferences lists every image the database refers to.
	ImageReferences(ctx context.Context) ([]Reference, error)
	// ImageReferenced reports whether the database refers to r right now.
	ImageReferenced(ctx context.Context, r Reference) (bool, error)
}

// Report says what a Reconcile or Purge run did.
type Report struct {
	Restored int // referenced images moved back from trash
	Trashed  int // unreferenced live images moved to trash
	Purged   int // trash entries older than the retention, removed
	// Warnings are entries that were left alone: missing images
	// (ErrMissingImage) and entries of unknown shape (ErrUnexpectedEntry).
	Warnings []error
	// Failures are entries that could not be processed, for example
	// because of an I/O error.
	Failures []error
}

func (r *Report) warn(path string, err error) {
	r.Warnings = append(r.Warnings, &ItemError{Path: path, Err: err})
}

func (r *Report) fail(path string, err error) {
	r.Failures = append(r.Failures, &ItemError{Path: path, Err: err})
}

// trashEntry is one deleted image in trash.
type trashEntry struct {
	ref     Reference
	path    string
	deleted time.Time
}

// rel names path for reports: relative to the data directory, with forward
// slashes.
func (d *Disk) rel(path string) string {
	rel, err := filepath.Rel(d.root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// trash moves the live image r to trash, named after the time of deletion,
// which survives copies and restarts unlike an mtime. A missing image is not
// an error, so deleting twice does not extend the retention. The caller
// holds d.mu.
func (d *Disk) trash(r Reference) error {
	from := d.path(r.Kind, r.OwnerID, r.Hash)
	if _, err := os.Lstat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("files: stat deleted image: %w", err)
	}
	dir := filepath.Join(d.root, trashDir, string(r.Kind), r.OwnerID.String(), r.Hash)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("files: create trash directory: %w", err)
	}
	to := filepath.Join(dir, strconv.FormatInt(d.now().UnixNano(), 10)+r.Kind.suffix())
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("files: move image to trash: %w", err)
	}
	syncDir(dir)
	syncDir(filepath.Dir(from))
	return nil
}

// walk calls visit for every regular file under dir (a directory of the
// data directory) with its path split into components relative to dir.
// Directories deeper than depth, and entries that are neither regular files
// nor directories, are left alone with a warning; unreadable directories
// are failures. Only the end of ctx stops it.
func (d *Disk) walk(ctx context.Context, dir string, depth int, rep *Report, visit func(path string, parts []string, entry fs.DirEntry)) error {
	base := filepath.Join(d.root, dir)
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if path != base || !errors.Is(err, fs.ErrNotExist) {
				rep.fail(d.rel(path), fmt.Errorf("read: %w", err))
			}
			return nil
		}
		if path == base {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			rep.fail(d.rel(path), fmt.Errorf("relative path: %w", err))
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		switch {
		case entry.IsDir() && len(parts) <= depth:
			return nil
		case entry.IsDir():
			rep.warn(d.rel(path), ErrUnexpectedEntry)
			return filepath.SkipDir
		case !entry.Type().IsRegular():
			rep.warn(d.rel(path), ErrUnexpectedEntry)
			return nil
		}
		visit(path, parts, entry)
		return nil
	})
	if err != nil {
		return fmt.Errorf("files: walk %s: %w", dir, err)
	}
	return nil
}

// parseReference builds a Reference from path components, reporting
// whether they form a valid one.
func parseReference(kind, owner, hash string) (Reference, bool) {
	id, err := snowflake.Parse(owner)
	r := Reference{Kind: Kind(kind), OwnerID: id, Hash: hash}
	return r, err == nil && id.String() == owner && r.valid()
}

// trashEntries lists trash. Entries of any other shape than
// <kind>/<owner_id>/<hash>/<unix_nano>.png are warned about and skipped.
func (d *Disk) trashEntries(ctx context.Context, rep *Report) ([]trashEntry, error) {
	var entries []trashEntry
	err := d.walk(ctx, trashDir, 3, rep, func(path string, parts []string, _ fs.DirEntry) {
		if len(parts) != 4 || filepath.Ext(parts[3]) != Kind(parts[0]).suffix() {
			rep.warn(d.rel(path), ErrUnexpectedEntry)
			return
		}
		r, ok := parseReference(parts[0], parts[1], parts[2])
		n, err := strconv.ParseInt(strings.TrimSuffix(parts[3], r.Kind.suffix()), 10, 64)
		if !ok || err != nil || n <= 0 {
			rep.warn(d.rel(path), ErrUnexpectedEntry)
			return
		}
		entries = append(entries, trashEntry{ref: r, path: path, deleted: time.Unix(0, n)})
	})
	return entries, err
}

// Reconcile makes the data directory match the database: it moves every
// image the database refers to back from trash, moves live images that
// nothing refers to into trash, and then purges trash (see Purge). It runs
// after a restore, and it is safe while the API is running:
//   - a live file goes to trash only if it is unreferenced and its mtime is
//     older than the run's start minus ReconcileGrace;
//   - the reference is checked again just before each move, under d.mu.
//
// Problems with single entries go into the report and never stop the run.
// The error is for what does stop it: the references cannot be read (then
// no file has been touched), or ctx ends.
func (d *Disk) Reconcile(ctx context.Context, refs References) (Report, error) {
	var rep Report
	start := d.now()
	list, err := refs.ImageReferences(ctx)
	if err != nil {
		return rep, fmt.Errorf("files: list references: %w", err)
	}
	entries, err := d.trashEntries(ctx, &rep)
	if err != nil {
		return rep, err
	}
	newest := make(map[Reference]trashEntry, len(entries))
	for _, e := range entries {
		if n, ok := newest[e.ref]; !ok || e.deleted.After(n.deleted) {
			newest[e.ref] = e
		}
	}
	wanted := make(map[Reference]bool, len(list))
	for _, r := range list {
		if !r.valid() {
			rep.warn(fmt.Sprintf("%s/%s/%s", r.Kind, r.OwnerID, r.Hash), ErrUnexpectedEntry)
			continue
		}
		wanted[r] = true
		d.restore(r, newest, &rep)
	}
	cutoff := start.Add(-ReconcileGrace)
	for _, kind := range []Kind{KindAvatar, KindIcon, KindAttachment} {
		err = d.walk(ctx, string(kind), 1, &rep, func(path string, parts []string, entry fs.DirEntry) {
			if len(parts) == 2 && strings.HasPrefix(parts[1], ".upload-") {
				return // an upload in progress, or one a crash left behind
			}
			r, ok := Reference{}, false
			if len(parts) == 2 && filepath.Ext(parts[1]) == kind.suffix() {
				r, ok = parseReference(string(kind), parts[0], strings.TrimSuffix(parts[1], kind.suffix()))
			}
			if !ok {
				rep.warn(d.rel(path), ErrUnexpectedEntry)
				return
			}
			if wanted[r] {
				return
			}
			info, statErr := entry.Info()
			if statErr != nil {
				rep.fail(d.rel(path), fmt.Errorf("stat: %w", statErr))
				return
			}
			if info.ModTime().Before(cutoff) {
				d.trashOrphan(ctx, refs, r, cutoff, &rep)
			}
		})
		if err != nil {
			return rep, err
		}
	}
	// The first scan of trash above already warned about its entries.
	var purged Report
	err = d.purge(ctx, &purged)
	rep.Purged = purged.Purged
	rep.Failures = append(rep.Failures, purged.Failures...)
	return rep, err
}

// restore makes sure the referenced image r is live, moving its newest
// copy back from trash if it is not.
func (d *Disk) restore(r Reference, newest map[Reference]trashEntry, rep *Report) {
	d.mu.Lock()
	defer d.mu.Unlock()
	live := d.path(r.Kind, r.OwnerID, r.Hash)
	info, err := os.Lstat(live)
	switch {
	case err == nil && info.Mode().IsRegular():
		return
	case err == nil:
		rep.warn(d.rel(live), ErrUnexpectedEntry)
		return
	case !errors.Is(err, fs.ErrNotExist):
		rep.fail(d.rel(live), fmt.Errorf("stat: %w", err))
		return
	}
	e, ok := newest[r]
	if !ok {
		rep.warn(d.rel(live), ErrMissingImage)
		return
	}
	if err := os.MkdirAll(filepath.Dir(live), dirPerm); err != nil {
		rep.fail(d.rel(live), fmt.Errorf("create directory: %w", err))
		return
	}
	if err := os.Rename(e.path, live); err != nil {
		rep.fail(d.rel(e.path), fmt.Errorf("move back from trash: %w", err))
		return
	}
	syncDir(filepath.Dir(live))
	rep.Restored++
}

// trashOrphan moves the live image r to trash unless, by now, the database
// refers to it or the file was replaced after cutoff (an upload of the same
// image writes a new file).
func (d *Disk) trashOrphan(ctx context.Context, refs References, r Reference, cutoff time.Time, rep *Report) {
	d.mu.Lock()
	defer d.mu.Unlock()
	live := d.path(r.Kind, r.OwnerID, r.Hash)
	referenced, err := refs.ImageReferenced(ctx, r)
	if err != nil {
		rep.fail(d.rel(live), fmt.Errorf("check reference: %w", err))
		return
	}
	if referenced {
		return
	}
	info, err := os.Lstat(live)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		rep.fail(d.rel(live), fmt.Errorf("stat: %w", err))
		return
	case !info.ModTime().Before(cutoff):
		return
	}
	if err := d.trash(r); err != nil {
		rep.fail(d.rel(live), err)
		return
	}
	rep.Trashed++
}

// Purge removes the trash entries that have been in trash longer than the
// retention. It never touches live images, and with no retention it
// removes nothing. Problems with single entries go into the report; the
// error is for ctx ending.
func (d *Disk) Purge(ctx context.Context) (Report, error) {
	var rep Report
	err := d.purge(ctx, &rep)
	return rep, err
}

// purge implements Purge, adding to rep.
func (d *Disk) purge(ctx context.Context, rep *Report) error {
	if d.retention <= 0 {
		return nil
	}
	entries, err := d.trashEntries(ctx, rep)
	if err != nil {
		return err
	}
	cutoff := d.now().Add(-d.retention)
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("files: purge: %w", err)
		}
		if e.deleted.Before(cutoff) {
			d.remove(e, rep)
		}
	}
	return nil
}

// remove deletes one trash entry, and its hash directory once empty.
func (d *Disk) remove(e trashEntry, rep *Report) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.Remove(e.path); err != nil {
		rep.fail(d.rel(e.path), fmt.Errorf("remove: %w", err))
		return
	}
	// Fails while other deletions of the same image remain; that is fine.
	_ = os.Remove(filepath.Dir(e.path))
	rep.Purged++
}

// RunPurge purges trash once at the start and then every interval, until
// ctx ends. Every problem a pass reports goes to onProblem. The caller
// waits for it to return during shutdown.
func (d *Disk) RunPurge(ctx context.Context, interval time.Duration, onProblem func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		rep, err := d.Purge(ctx)
		if ctx.Err() != nil {
			return
		}
		for _, p := range rep.Warnings {
			onProblem(p)
		}
		for _, p := range rep.Failures {
			onProblem(p)
		}
		if err != nil {
			onProblem(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
