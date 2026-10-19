//go:build darwin

package corepackage

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// statKey fingerprints an installed archive and returns its ctime. Any later
// rewrite, including a same-size one that restores mtime, changes ctime.
func statKey(info os.FileInfo) (string, time.Time) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return "", time.Time{}
	}
	changed := time.Unix(int64(stat.Ctimespec.Sec), int64(stat.Ctimespec.Nsec))
	return fmt.Sprintf("size=%d mtime=%d mode=%d dev=%d ino=%d ctime=%d", info.Size(), info.ModTime().UnixNano(),
		uint64(info.Mode()), uint64(stat.Dev), uint64(stat.Ino), changed.UnixNano()), changed
}
