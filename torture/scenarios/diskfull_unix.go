//go:build !windows
// +build !windows

package scenarios

import (
	"fmt"
	"syscall"
)

// getAvailableDiskSpace returns available disk space in bytes on Unix systems
func getAvailableDiskSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}

	// Validate block size before converting from potentially signed type to uint64
	// On Linux, Bsize is int64; on Darwin it's uint32
	if stat.Bsize <= 0 {
		return 0, fmt.Errorf("invalid block size: %d", stat.Bsize)
	}

	// Safe conversion after validation
	blockSize := uint64(stat.Bsize)

	// Available space = available blocks * block size
	return stat.Bavail * blockSize, nil
}

// isNoSpaceError checks if the error is a "no space left on device" error
func isNoSpaceError(err error) bool {
	return err == syscall.ENOSPC
}
