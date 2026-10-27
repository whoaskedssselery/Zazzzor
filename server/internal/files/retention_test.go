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
	err := filepath.WalkDir(filepath.Join(root, "trash"), func(_ string, entry fs.DirEntry, err error) error {
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
