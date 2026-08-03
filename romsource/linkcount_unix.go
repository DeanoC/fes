//go:build darwin || linux

package romsource

import (
	"io/fs"
	"syscall"
)

func preparedLinkCount(info fs.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Nlink), true
}
