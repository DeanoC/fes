package misterruntime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func writeAtomicDevelopmentRBF(path string, size int64, content io.Reader) error {
	if path == "" || size < 1 || content == nil {
		return fmt.Errorf("invalid development RBF input")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create development RBF directory: %w", err)
	}
	temporaryPath := path + ".new"
	defer os.Remove(temporaryPath)
	f, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open temporary development RBF: %w", err)
	}
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
	return nil
}
