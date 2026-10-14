//go:build unix

package kitpackages

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func (s *Store) lock(ctx context.Context) (*os.Root, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("kit packages: missing context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateRoot(r); err != nil {
		r.Close()
		return nil, nil, err
	}
	info, err := r.Lstat(".lock")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !os.SameFile(s.lockInfo, info) {
		r.Close()
		return nil, nil, errors.New("kit packages: lock changed")
	}
	f, err := r.OpenFile(".lock", os.O_RDWR, 0)
	if err != nil {
		r.Close()
		return nil, nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(s.lockInfo, opened) {
		f.Close()
		r.Close()
		return nil, nil, errors.New("kit packages: lock changed while opening")
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			f.Close()
			r.Close()
			return nil, nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			r.Close()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	unlock := func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close(); _ = r.Close() }
	// A process blocked on an old lock inode must never mutate the replacement
	// store after somebody changes its root or lock path.
	if err := s.validateRoot(r); err != nil {
		unlock()
		return nil, nil, err
	}
	current, err := r.Lstat(".lock")
	if err != nil || !os.SameFile(opened, current) {
		unlock()
		return nil, nil, errors.New("kit packages: lock replaced while waiting")
	}
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, nil, err
	}
	if err := cleanTemporaries(r); err != nil {
		unlock()
		return nil, nil, err
	}
	return r, unlock, nil
}
