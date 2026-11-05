//go:build linux

package kitpackages

import (
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
