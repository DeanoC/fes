//go:build windows

package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanLeaseDirectoryModeAcceptsWindowsModeBits(t *testing.T) {
	if !scanLeaseDirectoryModeIsPrivate(os.FileMode(0o777)) {
		t.Fatal("Windows directory mode bits were treated as Unix permissions")
	}
}

func TestPrepareScanLeaseDirectoryOnWindows(t *testing.T) {
	leaseDirectory := filepath.Join(t.TempDir(), "catalog.scan-locks")
	store := &Store{scanLeaseDirectory: leaseDirectory}
	if err := store.prepareScanLeaseDirectory(); err != nil {
		t.Fatalf("prepareScanLeaseDirectory: %v", err)
	}
	info, err := os.Lstat(leaseDirectory)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("lease path mode = %v, want directory", info.Mode())
	}
}
