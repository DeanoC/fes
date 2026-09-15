//go:build linux

package linuxinput

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Discover lists /dev/input/event* and /dev/input/js* in sorted order.
func Discover() ([]string, error) {
	var paths []string
	for _, pat := range []string{"/dev/input/event*", "/dev/input/js*"} {
		found, err := filepath.Glob(pat)
		if err != nil {
			return nil, err
		}
		paths = append(paths, found...)
	}
	sort.Strings(paths)
	return paths, nil
}

func deviceName(f *os.File) (string, error) {
	var buf [256]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), eviocgname(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", errno
	}
	n := bytes.IndexByte(buf[:], 0)
	if n < 0 {
		n = len(buf)
	}
	return string(buf[:n]), nil
}

// EVIOCGNAME(len) = _IOC(_IOC_READ, 'E', 0x06, len). x/sys does not export
// EVIOCGNAME on linux/arm.
func eviocgname(length int) uintptr {
	const (
		iocRead      = 2
		iocNrshift   = 0
		iocTypeshift = 8
		iocSizeshift = 16
		iocDirshift  = 30
	)
	return uintptr(iocRead)<<iocDirshift |
		uintptr('E')<<iocTypeshift |
		uintptr(0x06)<<iocNrshift |
		uintptr(length)<<iocSizeshift
}
