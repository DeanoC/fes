package mister

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func WriteAtomicMGL(directory string, content []byte) (string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create MGL directory: %w", err)
	}
	finalPath := filepath.Join(directory, "launch.mgl")
	temporaryPath := finalPath + ".new"
	defer os.Remove(temporaryPath)
	f, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("open temporary MGL: %w", err)
	}
	if _, err = f.Write(content); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", fmt.Errorf("write temporary MGL: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close temporary MGL: %w", closeErr)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("install MGL: %w", err)
	}
	return finalPath, nil
}

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
	if _, err = io.CopyN(f, content, size); err == nil {
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
