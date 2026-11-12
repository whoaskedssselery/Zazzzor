//go:build linux || darwin || freebsd

package files

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// volumeAvailableBytes returns filesystem blocks available to unprivileged writes.
func volumeAvailableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs: %w", err)
	}
	if stat.Bsize <= 0 || stat.Bavail <= 0 {
		return 0, nil
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
