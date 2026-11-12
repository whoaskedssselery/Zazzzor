//go:build !linux && !darwin && !freebsd && !windows

package files

import "errors"

// volumeAvailableBytes reports that this platform has no files volume probe.
func volumeAvailableBytes(string) (uint64, error) {
	return 0, errors.New("files: available space probe is unsupported on this platform")
}
