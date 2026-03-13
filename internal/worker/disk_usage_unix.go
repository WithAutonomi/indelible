//go:build !windows

package worker

import "syscall"

func getDiskUsagePercent(path string) float64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return -1
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	if total == 0 {
		return -1
	}
	used := total - free
	return float64(used) / float64(total) * 100.0
}
