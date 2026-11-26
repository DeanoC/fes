//go:build linux

package kitpackages

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestPersistentStorageRejectsTemporaryRootBeforeCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "library")
	if _, err := OpenPersistent(root); err == nil {
		t.Fatal("temporary production root accepted")
	}
	s := newStore(t)
	if err := s.CheckPersistent(); err == nil {
		t.Fatal("generic store claimed persistent")
	}
}

func TestHostPersistentOpenHonorsCancelledContextBeforeCheckingStorage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenPersistentForHost(ctx, filepath.Join(t.TempDir(), "library")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled host open = %v", err)
	}
}
