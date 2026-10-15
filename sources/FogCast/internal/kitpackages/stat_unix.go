//go:build unix

package kitpackages

import (
	"os"
	"syscall"
)

func inodeOf(info os.FileInfo) uint64 {
	switch stat := info.Sys().(type) {
	case *syscall.Stat_t:
		if stat != nil {
			return uint64(stat.Ino)
		}
	case syscall.Stat_t:
		return uint64(stat.Ino)
	}
	return 0
}
