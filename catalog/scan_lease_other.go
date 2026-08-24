//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package catalog

import (
	"errors"
	"os"
)

func tryLockScanLease(*os.File) (bool, error) {
	return false, errors.New("catalog cross-process scan leases are unsupported on this platform")
}

func unlockScanLease(*os.File) error {
	return nil
}

func scanLeaseDirectoryModeIsPrivate(mode os.FileMode) bool {
	return mode.Perm()&0o077 == 0
}
