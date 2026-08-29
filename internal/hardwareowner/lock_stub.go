//go:build !linux

package hardwareowner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Locker is present on non-Linux targets so callers can compile portably, but
// the owner lock fails closed because this package has no portable flock
// implementation.
type Locker struct {
	Path        string
	ExpectedUID uint32
}

func NewLocker(path string, expectedUID ...uint32) *Locker {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &Locker{Path: path, ExpectedUID: uid}
}

func NewProductionLocker() *Locker { return NewLocker(LockPath) }

func (l Locker) Lock(ctx context.Context) (Unlock, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("hardware-owner flock is unsupported on this platform")
}

func (l Locker) LockFile(ctx context.Context) (*os.File, Unlock, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("hardware-owner flock is unsupported on this platform")
}

func statUID(info os.FileInfo) (uint32, bool) { return 0, info != nil }
func processUID() uint32                      { return 0 }
func openOwnerRead(path string) (*os.File, error) {
	return os.Open(path)
}
func openLockNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}
func dirName(path string) string { return filepath.Dir(path) }
