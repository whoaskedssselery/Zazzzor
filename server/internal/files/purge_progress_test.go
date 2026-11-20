package files_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// walkBudget cancels deterministically while a directory is still being visited.
type walkBudget struct {
	context.Context
	checks int
}

func (c *walkBudget) Err() error {
	c.checks++
	if c.checks >= 20 {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestPurgeMakesProgressBeforeTraversalCompletes(t *testing.T) {
	t.Parallel()
	disk, root, clock := newClockDisk(t, time.Hour)
	dir := filepath.Join(root, "trash", "attachments", "1", "2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make trash: %v", err)
	}
	for i := range 100 {
		name := fmt.Sprintf("%d", clock.now.Add(-2*time.Hour).UnixNano()+int64(i))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o600); err != nil {
			t.Fatalf("write trash: %v", err)
		}
	}
	report, err := disk.Purge(&walkBudget{Context: t.Context()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Purge error = %v, want cancellation", err)
	}
	if report.Purged == 0 || report.Purged == 100 {
		t.Errorf("purged before traversal stopped = %d, want partial progress", report.Purged)
	}
}
