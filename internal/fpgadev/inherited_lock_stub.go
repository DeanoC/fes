//go:build !linux || !fpgadev

package fpgadev

import (
	"errors"
	"os"
)

func validateInheritedInstallLockFD(int, string) error {
	return errors.New("inherited install lock is unsupported on this platform")
}

func retainInheritedInstallLockFD(int, string) (*os.File, error) {
	return nil, errors.New("inherited install lock is unsupported on this platform")
}
