package files

import (
	"fmt"
	"sync"
	"time"
)

const freeSpaceTTL = 3 * time.Second

type freeSpaceSample struct {
	mu    sync.Mutex
	bytes uint64
	until time.Time
}

// AvailableBytes samples free space available to the API user on the files
// volume. Concurrent uploads share the sample for up to three seconds.
func (d *Disk) AvailableBytes() (uint64, error) {
	sample := &d.freeSpace
	sample.mu.Lock()
	defer sample.mu.Unlock()
	now := d.now()
	if now.Before(sample.until) {
		return sample.bytes, nil
	}
	free, err := volumeAvailableBytes(d.root)
	if err != nil {
		return 0, fmt.Errorf("files: check volume space: %w", err)
	}
	sample.bytes, sample.until = free, now.Add(freeSpaceTTL)
	return free, nil
}
