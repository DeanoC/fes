package mister

import (
	"fmt"
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
