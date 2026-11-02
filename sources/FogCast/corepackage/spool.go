package corepackage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// SpooledInput is one launch body written once to a private file in the
// staging root and read back through a read-only mapping (#863). Staging reads
// its bytes without a heap copy, and a ROM input publishes the same file as its
// input.tar by hard link instead of writing the envelope again. The bytes stay
// valid until Close; nothing staged from them may keep a reference.
type SpooledInput struct {
	data     []byte
	unmap    func() error
	root     string
	rootInfo os.FileInfo
	name     string
	once     sync.Once
	closeErr error
}

// SpoolInput copies exactly size bytes of r, at most limit, into the staging
// root. A short or long body, a cancelled context or a write error leaves no
// file behind.
func SpoolInput(ctx context.Context, root string, size, limit int64, r io.Reader) (*SpooledInput, error) {
	if ctx == nil || r == nil || size < 1 || size > limit {
		return nil, errors.New("core package: invalid input size")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("core package: staging root must be absolute")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("core package: staging root must be an existing non-symlink directory")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("core package: open staging root: %w", err)
	}
	defer handle.Close()
	opened, err := handle.Stat(".")
	if err != nil || !os.SameFile(rootInfo, opened) {
		return nil, errors.New("core package: staging root changed while opening")
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	name := ".input-" + token
	file, err := handle.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("core package: create input spool: %w", err)
	}
	kept := false
	defer func() {
		_ = file.Close()
		if !kept {
			_ = handle.Remove(name)
		}
	}()
	n, err := io.Copy(file, io.LimitReader(&contextReader{ctx: ctx, reader: r}, size+1))
	if err != nil {
		return nil, err
	}
	if n != size {
		return nil, errors.New("core package: upload length does not match the declared length")
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("core package: write input spool: %w", err)
	}
	// Published input.tar members are immutable and private; a link shares
	// this mode.
	if err := file.Chmod(0o400); err != nil {
		return nil, fmt.Errorf("core package: seal input spool: %w", err)
	}
	data, unmap, err := mapReadOnly(file, size)
	if err != nil {
		return nil, fmt.Errorf("core package: map input spool: %w", err)
	}
	kept = true
	return &SpooledInput{data: data, unmap: unmap, root: root, rootInfo: rootInfo, name: name}, nil
}

// Bytes returns the spooled body. It must not be used after Close.
func (s *SpooledInput) Bytes() []byte { return s.data }

// Close releases the mapping and removes the spool name. A published
// input.tar link keeps the file.
func (s *SpooledInput) Close() error {
	s.once.Do(func() {
		s.data = nil
		err := s.unmap()
		handle, openErr := os.OpenRoot(s.root)
		if openErr != nil {
			s.closeErr = errors.Join(err, openErr)
			return
		}
		defer handle.Close()
		if opened, statErr := handle.Stat("."); statErr != nil || !os.SameFile(s.rootInfo, opened) {
			s.closeErr = errors.Join(err, errors.New("core package: staging root changed before spool removal"))
			return
		}
		if removeErr := handle.Remove(s.name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
		s.closeErr = err
	})
	return s.closeErr
}

// writeInput publishes a transport envelope as name: by hard link when it is
// the spool in this staging root, otherwise as a new private file.
func writeInput(handle *os.Root, name string, data []byte, spool *SpooledInput) error {
	if spool == nil {
		return writePrivate(handle, name, data)
	}
	opened, err := handle.Stat(".")
	if err != nil || !os.SameFile(spool.rootInfo, opened) {
		return errors.New("core package: input spool is outside the staging root")
	}
	if err := handle.Link(spool.name, name); err != nil {
		return fmt.Errorf("core package: publish input spool: %w", err)
	}
	return nil
}
