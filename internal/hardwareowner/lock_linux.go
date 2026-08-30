//go:build linux

package hardwareowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// NewLocker constructs a Linux flock-backed owner lock. If expectedUID is
// omitted, root (0) is required.
func NewLocker(path string, expectedUID ...uint32) *Locker {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &Locker{Path: path, ExpectedUID: uid}
}

// NewProductionLocker returns the root-owned lock used by the target profile.
func NewProductionLocker() *Locker { return NewLocker(LockPath) }

// Locker serializes all owner-state admission and replacement operations with
// an exclusive, durable-path flock. The lock file itself is never removed.
type Locker struct {
	Path        string
	ExpectedUID uint32
}

// Lock acquires an exclusive lock while honoring cancellation. Nonblocking
// flock attempts are retried so a blocked syscall cannot outlive its context.
func (l Locker) Lock(ctx context.Context) (Unlock, error) {
	_, unlock, err := l.LockFile(ctx)
	return unlock, err
}

// LockExisting acquires a lock only when the protected lock file already
// exists. Maintenance admission uses this form so read-only operation never
// creates authority that only installation is allowed to establish.
func (l Locker) LockExisting(ctx context.Context) (Unlock, error) {
	_, unlock, err := l.lockFile(ctx, openExistingLockNoFollow)
	return unlock, err
}

// LockFile is the descriptor-retaining form of Lock. The returned descriptor
// is the exact protected lock file whose flock is held by unlock. It exists
// for the recovery trampoline's same-process exec handoff; ordinary callers
// should use Lock so the descriptor cannot accidentally escape its scope.
func (l Locker) LockFile(ctx context.Context) (*os.File, Unlock, error) {
	return l.lockFile(ctx, openLockNoFollow)
}

func (l Locker) lockFile(ctx context.Context, openLock func(string) (*os.File, error)) (*os.File, Unlock, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("lock context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if l.Path == "" {
		return nil, nil, fmt.Errorf("lock path is required")
	}
	if err := ensureSecureParent(dirName(l.Path), l.ExpectedUID); err != nil {
		return nil, nil, err
	}
	file, err := openLock(l.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("open owner lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("stat owner lock: %w", err)
	}
	if err := validateLockInfo(info, l.ExpectedUID); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if err := flockContext(ctx, file); err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	var once sync.Once
	var unlockErr error
	unlock := func() error {
		once.Do(func() {
			unlockErr = unix.Flock(int(file.Fd()), unix.LOCK_UN)
			if closeErr := file.Close(); unlockErr == nil {
				unlockErr = closeErr
			}
		})
		return unlockErr
	}
	return file, unlock, nil
}

func flockContext(ctx context.Context, file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
				return ctxErr
			}
			return nil
		}
		if err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			return fmt.Errorf("flock owner lock: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func validateLockInfo(info os.FileInfo, expectedUID uint32) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("owner lock is not a regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return fmt.Errorf("owner lock link count is not one")
	}
	if info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("owner lock mode is %o, want 600", info.Mode().Perm())
	}
	uid, ok := statUID(info)
	if !ok || uid != expectedUID {
		return fmt.Errorf("owner lock uid is %d, want %d", uid, expectedUID)
	}
	return nil
}

func statUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

func processUID() uint32 { return uint32(os.Getuid()) }

func openOwnerRead(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openLockNoFollow(path string) (*os.File, error) {
	file, err := openExistingLockNoFollow(path)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openExistingLockNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func dirName(path string) string { return filepath.Dir(path) }
