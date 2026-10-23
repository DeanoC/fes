//go:build linux

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func settleLinuxCtime(t *testing.T, path string, info os.FileInfo) {
	t.Helper()
	before, ok := linuxFileCtime(info)
	if !ok {
		t.Fatal("linux stat missing ctime")
	}
	deadline := time.Now().Add(time.Second)
	for {
		afterInfo, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		after, ok := linuxFileCtime(afterInfo)
		if ok && after != before {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("Linux ctime did not advance after a same-size rewrite")
		}
		time.Sleep(2 * time.Millisecond)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

func linuxFileCtime(info os.FileInfo) (int64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, false
	}
	return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec), true
}
