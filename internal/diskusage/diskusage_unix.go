//go:build !windows

// Package diskusage reports filesystem capacity for a given path. It returns
// raw byte figures (total/free/used) so callers can render usage and derive a
// percentage; the disk-alert worker and the admin System page both consume it.
package diskusage

import "syscall"

// Usage returns total, free, and used bytes for the filesystem backing path.
// ok is false when the figures can't be read (e.g. statfs failure), in which
// case callers should treat the disk stats as unavailable.
func Usage(path string) (total, free, used uint64, ok bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, false
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bfree * uint64(stat.Bsize)
	if total == 0 {
		return 0, 0, 0, false
	}
	return total, free, total - free, true
}
