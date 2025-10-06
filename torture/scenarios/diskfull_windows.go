//go:build windows
// +build windows

package scenarios

import (
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// getAvailableDiskSpace returns available disk space in bytes on Windows
func getAvailableDiskSpace(path string) (uint64, error) {
	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	//nolint:gosec // G103: Windows syscall requires unsafe pointer conversions
	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		return 0, err
	}

	return freeBytesAvailable, nil
}

// isNoSpaceError checks if the error is a "disk full" error on Windows
func isNoSpaceError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		// ERROR_DISK_FULL (112) or ERROR_HANDLE_DISK_FULL (39)
		return errno == 112 || errno == 39
	}
	return false
}
