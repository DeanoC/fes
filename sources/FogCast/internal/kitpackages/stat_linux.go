//go:build linux

package kitpackages

import (
	"os"
	"syscall"
)

func devOf(info os.FileInfo) uint64 {
	stat := linuxStat(info)
	if stat == nil {
		return 0
	}
	return uint64(stat.Dev)
}

func ctimeOf(info os.FileInfo) int64 {
	stat := linuxStat(info)
	if stat == nil {
		return 0
	}
	return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec)
}

func linuxStat(info os.FileInfo) *syscall.Stat_t {
	if info == nil || info.Sys() == nil {
		return nil
	}
	switch stat := info.Sys().(type) {
	case *syscall.Stat_t:
		return stat
	case syscall.Stat_t:
		copied := stat
		return &copied
	default:
		return nil
	}
}
