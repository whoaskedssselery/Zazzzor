package files_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// clock is a settable time source for files.Options.Now.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// newClockDisk is a Disk on a temporary root whose clock the test moves.
func newClockDisk(t *testing.T, retention time.Duration) (*files.Disk, string, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 10, 27, 4, 30, 0, 0, time.UTC)}
	root := t.TempDir()
	d, err := files.NewDisk(root, files.Options{Retention: retention, Now: c.Now})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	return d, root, c
}

// refs is a files.References over a fixed list. now, when set, answers
// ImageReferenced instead of the list, as if the database changed after the
// list was read.
type refs struct {
	list []files.Reference
	now  map[files.Reference]bool
	err  error
}

func (r *refs) ImageReferences(context.Context) ([]files.Reference, error) {
	return r.list, r.err
}

func (r *refs) ImageReferenced(_ context.Context, ref files.Reference) (bool, error) {
	if r.now != nil {
		return r.now[ref], nil
	}
	for _, l := range r.list {
		if l == ref {
			return true, nil
		}
	}
	return false, nil
}

// saveAt stores a small image (its content varies with shade) and sets its
// mtime to at.
func saveAt(t *testing.T, d *files.Disk, root string, kind files.Kind, ownerID snowflake.ID, shade int, at time.Time) files.Reference {
	t.Helper()
	hash, err := d.SaveImage(t.Context(), kind, ownerID, bytes.NewReader(encodePNG(t, gradient(8+shade, 8))))
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	ref := files.Reference{Kind: kind, OwnerID: ownerID, Hash: hash}
	if err := os.Chtimes(livePath(root, ref), at, at); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return ref
}

func livePath(root string, r files.Reference) string {
	return filepath.Join(root, string(r.Kind), r.OwnerID.String(), r.Hash+".png")
}

func isLive(t *testing.T, root string, r files.Reference) bool {
	t.Helper()
	_, err := os.Stat(livePath(root, r))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat: %v", err)
	}
	return err == nil
}

// trashCount counts the files under trash/.
func trashCount(t *testing.T, root string) int {
	t.Helper()
	n := 0
	// A purge may run while this counts: entries it removed meanwhile are
	// simply gone, not an error.
	err := filepath.WalkDir(filepath.Join(root, "trash"), func(_ string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && entry.Type().IsRegular() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	return n
}

func reconcile(t *testing.T, d *files.Disk, r files.References) files.Report {
	t.Helper()
	rep, err := d.Reconcile(t.Context(), r)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return rep
}

func TestDeleteMovesImageToTrash(t *testing.T) {
	disk, root := newDisk(t)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, time.Now())
	want, err := os.ReadFile(livePath(root, ref))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err = disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if isLive(t, root, ref) {
		t.Error("deleted image is still live")
	}
	var got []byte
	err = filepath.WalkDir(filepath.Join(root, "trash"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			got, err = os.ReadFile(path) //nolint:gosec // G304: enumerated path under t.TempDir.
		}
		return err
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("trash holds %d bytes, want the %d bytes of the deleted image", len(got), len(want))
	}
}

func TestDeleteOfMissingImageIsNoop(t *testing.T) {
	disk, root := newDisk(t)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, time.Now())
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if n := trashCount(t, root); n != 1 {
		t.Errorf("trash entries = %d, want 1", n)
	}
}

func TestReconcileRestoresReferencedImageFromTrash(t *testing.T) {
	disk, root, _ := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, time.Now())
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// A new Disk on the same root: the deletion survives a restart.
	disk, err := files.NewDisk(root, files.Options{})
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	rep := reconcile(t, disk, &refs{list: []files.Reference{ref}})
	if !isLive(t, root, ref) {
		t.Error("referenced image was not moved back from trash")
	}
	if rep.Restored != 1 {
		t.Errorf("Restored = %d, want 1", rep.Restored)
	}
}

func TestReconcileTrashesOldOrphan(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindIcon, owner, 0, c.now.Add(-files.ReconcileGrace-time.Minute))
	rep := reconcile(t, disk, &refs{})
	if isLive(t, root, ref) {
		t.Error("old orphan is still live")
	}
	if rep.Trashed != 1 {
		t.Errorf("Trashed = %d, want 1", rep.Trashed)
	}
	if n := trashCount(t, root); n != 1 {
		t.Errorf("trash entries = %d, want 1", n)
	}
}

func TestReconcileKeepsRecentOrphan(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindIcon, owner, 0, c.now.Add(-files.ReconcileGrace+time.Minute))
	rep := reconcile(t, disk, &refs{})
	if !isLive(t, root, ref) {
		t.Error("an unreferenced file written within the grace window was moved to trash")
	}
	if rep.Trashed != 0 {
		t.Errorf("Trashed = %d, want 0", rep.Trashed)
	}
}

func TestReconcileKeepsFileReferencedDuringRun(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now.Add(-2*files.ReconcileGrace))
	// The list says nothing refers to it; by the time of the move, the
	// database does.
	rep := reconcile(t, disk, &refs{now: map[files.Reference]bool{ref: true}})
	if !isLive(t, root, ref) {
		t.Error("a file that became referenced during the run was moved to trash")
	}
	if rep.Trashed != 0 {
		t.Errorf("Trashed = %d, want 0", rep.Trashed)
	}
}

func TestReconcileRestoresTrashedOrphanReferencedAgain(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindIcon, owner, 0, c.now.Add(-2*files.ReconcileGrace))
	reconcile(t, disk, &refs{})
	// An older database that still refers to the orphan gets it back.
	rep := reconcile(t, disk, &refs{list: []files.Reference{ref}})
	if !isLive(t, root, ref) {
		t.Error("a trashed orphan referenced again was not moved back")
	}
	if rep.Restored != 1 {
		t.Errorf("Restored = %d, want 1", rep.Restored)
	}
}

func TestReconcileWarnsAboutMissingReference(t *testing.T) {
	disk, root, _ := newClockDisk(t, 0)
	missing := files.Reference{Kind: files.KindAvatar, OwnerID: owner, Hash: "0123456789abcdef"}
	present := saveAt(t, disk, root, files.KindIcon, owner, 0, time.Now())
	if err := disk.Delete(present.Kind, present.OwnerID, present.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rep := reconcile(t, disk, &refs{list: []files.Reference{missing, present}})
	if !isLive(t, root, present) {
		t.Error("a missing reference stopped the restore of the others")
	}
	if len(rep.Warnings) != 1 || !errors.Is(rep.Warnings[0], files.ErrMissingImage) {
		t.Errorf("Warnings = %v, want one ErrMissingImage", rep.Warnings)
	}
	if len(rep.Failures) != 0 {
		t.Errorf("Failures = %v, want none", rep.Failures)
	}
}

func TestReconcileLeavesUnexpectedEntriesAlone(t *testing.T) {
	disk, root, c := newClockDisk(t, time.Hour)
	if err := os.MkdirAll(filepath.Join(root, "avatars", owner.String()), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stray := []string{
		filepath.Join(root, "trash", "notes.txt"),
		filepath.Join(root, "avatars", owner.String(), "readme.txt"),
	}
	old := c.now.Add(-48 * time.Hour)
	for _, p := range stray {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}
	rep := reconcile(t, disk, &refs{})
	for _, p := range stray {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("unexpected entry %s was touched: %v", p, err)
		}
	}
	if len(rep.Warnings) != 2 {
		t.Errorf("Warnings = %v, want 2", rep.Warnings)
	}
	for _, w := range rep.Warnings {
		if !errors.Is(w, files.ErrUnexpectedEntry) {
			t.Errorf("warning %v, want ErrUnexpectedEntry", w)
		}
	}
}

func TestReconcileContinuesPastFailedItem(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	old := c.now.Add(-2 * files.ReconcileGrace)
	bad := saveAt(t, disk, root, files.KindIcon, owner, 0, old)
	good := saveAt(t, disk, root, files.KindIcon, owner+1, 1, old)
	deleted := saveAt(t, disk, root, files.KindAvatar, owner, 2, old)
	if err := disk.Delete(deleted.Kind, deleted.OwnerID, deleted.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// A file where bad's trash directory must go makes moving bad fail.
	blocker := filepath.Join(root, "trash", string(bad.Kind), bad.OwnerID.String())
	if err := os.MkdirAll(filepath.Dir(blocker), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rep := reconcile(t, disk, &refs{list: []files.Reference{deleted}})
	if len(rep.Failures) != 1 {
		t.Fatalf("Failures = %v, want 1", rep.Failures)
	}
	if !isLive(t, root, bad) {
		t.Error("the item that failed to move is gone")
	}
	if isLive(t, root, good) {
		t.Error("the orphan after the failed item was not moved to trash")
	}
	if !isLive(t, root, deleted) {
		t.Error("the referenced image was not restored")
	}
	if rep.Trashed != 1 || rep.Restored != 1 {
		t.Errorf("Trashed, Restored = %d, %d, want 1, 1", rep.Trashed, rep.Restored)
	}
}

func TestReconcileTouchesNothingWhenReferencesFail(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindIcon, owner, 0, c.now.Add(-2*files.ReconcileGrace))
	if _, err := disk.Reconcile(t.Context(), &refs{err: errors.New("db down")}); err == nil {
		t.Fatal("Reconcile with failing references: want error, got nil")
	}
	if !isLive(t, root, ref) {
		t.Error("a file was moved although the references could not be read")
	}
}

func TestPurgeKeepsEntriesWithinRetention(t *testing.T) {
	disk, root, c := newClockDisk(t, 48*time.Hour)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now)
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	c.now = c.now.Add(48 * time.Hour)
	rep, err := disk.Purge(t.Context())
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n := trashCount(t, root); rep.Purged != 0 || n != 1 {
		t.Errorf("Purged = %d, trash = %d at exactly the retention, want 0, 1", rep.Purged, n)
	}
}

func TestPurgeRemovesExpiredEntries(t *testing.T) {
	disk, root, c := newClockDisk(t, 48*time.Hour)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now)
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	c.now = c.now.Add(48*time.Hour + time.Second)
	rep, err := disk.Purge(t.Context())
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n := trashCount(t, root); rep.Purged != 1 || n != 0 {
		t.Errorf("Purged = %d, trash = %d, want 1, 0", rep.Purged, n)
	}
}

func TestPurgeNeverTouchesLiveFiles(t *testing.T) {
	disk, root, c := newClockDisk(t, 48*time.Hour)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now.Add(-1000*time.Hour))
	c.now = c.now.Add(1000 * time.Hour)
	if _, err := disk.Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if !isLive(t, root, ref) {
		t.Error("Purge removed a live file")
	}
}

func TestPurgeWithoutRetentionKeepsTrash(t *testing.T) {
	disk, root, c := newClockDisk(t, 0)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now)
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	c.now = c.now.Add(10000 * time.Hour)
	if _, err := disk.Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n := trashCount(t, root); n != 1 {
		t.Errorf("trash entries = %d, want 1", n)
	}
}

func TestRunPurgePurgesAtStart(t *testing.T) {
	disk, root, c := newClockDisk(t, 48*time.Hour)
	ref := saveAt(t, disk, root, files.KindAvatar, owner, 0, c.now)
	if err := disk.Delete(ref.Kind, ref.OwnerID, ref.Hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	c.now = c.now.Add(49 * time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		disk.RunPurge(ctx, time.Hour, func(err error) { t.Errorf("RunPurge problem: %v", err) })
	}()
	deadline := time.Now().Add(5 * time.Second)
	for trashCount(t, root) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if n := trashCount(t, root); n != 0 {
		t.Errorf("trash entries = %d after the first pass, want 0", n)
	}
}

func TestRunPurgeStopsOnCancel(t *testing.T) {
	disk, _ := newDisk(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		disk.RunPurge(ctx, time.Millisecond, func(err error) { t.Errorf("RunPurge problem: %v", err) })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunPurge did not return after cancel")
	}
}
