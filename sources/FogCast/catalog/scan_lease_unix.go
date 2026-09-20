//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package catalog

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockScanLease(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockScanLease(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func scanLeaseDirectoryModeIsPrivate(mode os.FileMode) bool {
	return mode.Perm()&0o077 == 0
}
