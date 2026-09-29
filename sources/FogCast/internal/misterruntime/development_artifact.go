package misterruntime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/internal/atomicwrite"
)

func writeAtomicDevelopmentRBF(path string, size int64, content io.Reader) error {
	if path == "" || size < 1 || content == nil {
		return fmt.Errorf("invalid development RBF input")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create development RBF directory: %w", err)
	}
	// Not path+".new": on a FAT-backed path the kit's exFAT driver resolves
	// that name to path itself, so the deferred cleanup would delete the
	// installed RBF (#317). Use a dot-prefixed O_EXCL sibling instead.
	f, err := os.CreateTemp(filepath.Dir(path), atomicwrite.TempPattern(path))
	if err != nil {
		return fmt.Errorf("open temporary development RBF: %w", err)
	}
	temporaryPath := f.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = f.Chmod(0o600); err == nil {
		_, err = io.CopyN(f, content, size)
	}
	if err == nil {
		var trailing [1]byte
		count, readErr := content.Read(trailing[:])
		switch {
		case count != 0:
			err = fmt.Errorf("development RBF exceeds declared size")
		case readErr == io.EOF:
		case readErr != nil:
			err = readErr
		default:
			err = io.ErrNoProgress
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("write temporary development RBF: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary development RBF: %w", closeErr)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install development RBF: %w", err)
	}
	installed = true
	atomicwrite.SyncDir(filepath.Dir(path))
	return nil
}
