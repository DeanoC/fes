//go:build unix

package host

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// peerStreamLive peeks without consuming. A past read deadline hides a queued
// FIN, so the probe uses a non-blocking peek instead.
func peerStreamLive(conn net.Conn) bool {
	sys, ok := conn.(syscall.Conn)
	if !ok || conn == nil {
		return conn != nil
	}
	raw, err := sys.SyscallConn()
	if err != nil {
		return false
	}
	live := false
	if err := raw.Read(func(fd uintptr) bool {
		var buf [1]byte
		n, _, recErr := unix.Recvfrom(int(fd), buf[:], unix.MSG_PEEK|unix.MSG_DONTWAIT)
		if recErr == unix.EAGAIN || recErr == unix.EWOULDBLOCK || n > 0 {
			live = true
		}
		return true
	}); err != nil {
		return false
	}
	return live
}
