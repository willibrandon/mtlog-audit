//go:build !windows
// +build !windows

package scenarios

import (
	"syscall"
)

// getAvailableDiskSpace returns available disk space in bytes on Unix systems
func getAvailableDiskSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Available space = available blocks * block size
	return stat.Bavail * uint64(stat.Bsize), nil
}

// isNoSpaceError checks if the error is a "no space left on device" error
func isNoSpaceError(err error) bool {
	return err == syscall.ENOSPC
}
