//go:build linux && fpgadev

package fpgadev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// validateInheritedInstallLockFD checks the descriptor passed by
// recover-install before any reset, owner admission, or child creation. The
// descriptor remains open for the supervisor lifetime; only its exact inode
// and protected metadata are accepted. A flock operation is deliberately
// non-blocking: a descriptor that was not retained by the trampoline becomes
// the holder here rather than creating an unlocked handoff.
func retainInheritedInstallLockFD(fd int, path string) (*os.File, error) {
	if err := validateRawInheritedInstallLockFD(fd, path); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "inherited-install-lock"), nil
}

func validateRawInheritedInstallLockFD(fd int, path string) error {
	if fd <= 2 || path == "" {
		return errors.New("inherited install lock descriptor is invalid")
	}
	if err := validateSecureJournalDir(filepath.Dir(path), uint32(os.Getuid())); err != nil {
		return fmt.Errorf("install lock parent is not protected: %w", err)
	}
	var fdInfo unix.Stat_t
	err := unix.Fstat(fd, &fdInfo)
	if err != nil {
		return fmt.Errorf("stat inherited install lock: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat install lock: %w", err)
	}
	uid, ok := journalUID(pathInfo)
	if !ok {
		return errors.New("install lock owner identity is unavailable")
	}
	if fdInfo.Mode&unix.S_IFMT != unix.S_IFREG || fdInfo.Mode&0o777 != 0o600 || fdInfo.Uid != uid || fdInfo.Nlink != 1 || fdInfo.Size > InstallJournalMaxBytes {
		return errors.New("inherited install lock metadata is not protected")
	}
	if err := validateJournalFileInfo(pathInfo, uid); err != nil {
		return fmt.Errorf("install lock metadata: %w", err)
	}
	pathStat, ok := pathInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(fdInfo.Dev) != uint64(pathStat.Dev) || uint64(fdInfo.Ino) != uint64(pathStat.Ino) {
		return errors.New("inherited descriptor is not the install lock")
	}
	// Probe the lock through a separately opened file description. This proves
	// that the trampoline retained the flock without touching (and therefore
	// without reacquiring) the inherited descriptor itself.
	contenderFD, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open install lock contender: %w", err)
	}
	probeErr := unix.Flock(contenderFD, unix.LOCK_EX|unix.LOCK_NB)
	closeErr := unix.Close(contenderFD)
	if probeErr == nil {
		if closeErr != nil {
			return fmt.Errorf("close unlocked install lock contender: %w", closeErr)
		}
		return errors.New("inherited descriptor does not hold install lock")
	}
	if !errors.Is(probeErr, unix.EWOULDBLOCK) && !errors.Is(probeErr, unix.EAGAIN) {
		if closeErr != nil {
			return fmt.Errorf("probe install lock contender: %w (close: %v)", probeErr, closeErr)
		}
		return fmt.Errorf("probe install lock contender: %w", probeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close install lock contender: %w", closeErr)
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		return fmt.Errorf("inspect inherited install lock flags: %w", err)
	}
	if flags&unix.FD_CLOEXEC != 0 {
		return errors.New("inherited install lock descriptor is close-on-exec")
	}
	return nil
}

func validateInheritedInstallLockFD(fd int, path string) error {
	return validateRawInheritedInstallLockFD(fd, path)
}
