//go:build windows

// Package diskusage reports filesystem capacity for a given path. It returns
// raw byte figures (total/free/used) so callers can render usage and derive a
// percentage; the disk-alert worker and the admin System page both consume it.
package diskusage

import (
	"syscall"
	"unsafe"
)

// Usage returns total, free, and used bytes for the filesystem backing path.
// ok is false when the figures can't be read (bad path / API failure), in
// which case callers should treat the disk stats as unavailable.
func Usage(path string) (total, free, used uint64, ok bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, false
	}

	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 || totalBytes == 0 {
		return 0, 0, 0, false
	}

	return totalBytes, totalFreeBytes, totalBytes - totalFreeBytes, true
}
