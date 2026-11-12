package files

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// volumeAvailableBytes returns the space available to the current Windows user.
func volumeAvailableBytes(path string) (uint64, error) {
	dir, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode volume path: %w", err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(dir, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("get disk free space: %w", err)
	}
	return available, nil
}
