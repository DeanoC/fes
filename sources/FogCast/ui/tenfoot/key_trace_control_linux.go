//go:build linux

package tenfoot

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	keyTraceArmRequest    = "/run/fogcast/tenfoot-key-trace-arm"
	keyTraceDumpRequest   = "/run/fogcast/tenfoot-key-trace-dump"
	keyTraceRequestMaxAge = 10 * time.Second
)

// consumeKeyTraceRequest accepts a fresh, owner-only regular request containing
// exactly this process's PID. Stale files and symlinks are left untouched; the
// requester owns their cleanup. No user-controlled path or text reaches a dump.
func keyTraceProcessStart() (string, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return "", err
	}
	close := strings.LastIndexByte(string(data), ')')
	if close < 0 {
		return "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[close+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("short process stat")
	}
	return fields[19], nil
}

func consumeKeyTraceRequest(path string, pid int, start string, uid int, now time.Time) bool {
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0o022 != 0 {
		return false
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || int(parentStat.Uid) != uid {
		return false
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || now.Before(info.ModTime()) || now.Sub(info.ModTime()) > keyTraceRequestMaxAge {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil || start == "" || string(body) != fmt.Sprintf("%d %s\n", pid, start) {
		return false
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return false
	}
	return os.Remove(path) == nil
}
