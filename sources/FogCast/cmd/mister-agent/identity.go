package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/internal/discovery"
)

// loadOrCreateTargetID returns the kit's stable node id. The persisted
// target-id file is that id. This does not mint a second identifier.
func loadOrCreateTargetID(path, configured string) (string, error) {
	if configured != "" {
		if !discovery.ValidID(configured) {
			return "", errors.New("configured target ID is invalid")
		}
		return configured, nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSuffix(string(data), "\n")
		if !discovery.ValidID(id) {
			return "", errors.New("persisted target ID is invalid")
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read persisted target ID: %w", err)
	}
	id, err := discovery.NewID()
	if err != nil {
		return "", err
	}
	if err := persistTargetID(path, id); err != nil {
		return "", err
	}
	return id, nil
}

func persistTargetID(path, id string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create target identity directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".target-id-*")
	if err != nil {
		return fmt.Errorf("create target identity: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure target identity: %w", err)
	}
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		return fmt.Errorf("write target identity: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync target identity: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close target identity: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install target identity: %w", err)
	}
	ok = true
	if dirHandle, openErr := os.Open(dir); openErr == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
