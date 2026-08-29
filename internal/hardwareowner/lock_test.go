package hardwareowner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testLocker(t *testing.T) Locker {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Locker{Path: filepath.Join(dir, "hardware-owner-v1.lock"), ExpectedUID: uint32(os.Getuid())}
}

func TestLockerExcludesConcurrentOwnersAndUnlocks(t *testing.T) {
	locker := testLocker(t)
	first, err := locker.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first() }()
	info, err := os.Lstat(locker.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode/type = %s, want regular 0600", info.Mode())
	}
	acquired := make(chan Unlock, 1)
	failed := make(chan error, 1)
	go func() {
		unlock, err := locker.Lock(context.Background())
		if err != nil {
			failed <- err
			return
		}
		acquired <- unlock
	}()
	select {
	case unlock := <-acquired:
		_ = unlock()
		t.Fatal("second owner acquired before first unlock")
	case err := <-failed:
		t.Fatalf("second owner failed before first unlock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first(); err != nil {
		t.Fatal(err)
	}
	select {
	case unlock := <-acquired:
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
	case err := <-failed:
		t.Fatalf("second owner failed after unlock: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("second owner did not acquire after unlock")
	}
}

func TestLockerLockHonorsContextCancellationWhileHeld(t *testing.T) {
	locker := testLocker(t)
	unlock, err := locker.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := locker.Lock(ctx); err == nil {
		t.Fatal("canceled lock acquired")
	} else if ctx.Err() == nil {
		t.Fatalf("lock returned before context cancellation: %v", err)
	} else if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("lock ignored held owner and returned too quickly: %s", elapsed)
	}
}

func TestLockerRejectsSymlinkAndWrongMode(t *testing.T) {
	locker := testLocker(t)
	target := filepath.Join(filepath.Dir(locker.Path), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), locker.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Lock(context.Background()); err == nil {
		t.Fatal("lock symlink accepted")
	}
	if err := os.Remove(locker.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(locker.Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Lock(context.Background()); err == nil {
		t.Fatal("world-readable lock accepted")
	}
}

func TestLockerRejectsUnsafeParent(t *testing.T) {
	root := t.TempDir()
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	locker := Locker{Path: filepath.Join(unsafe, "hardware-owner-v1.lock"), ExpectedUID: uint32(os.Getuid())}
	if _, err := locker.Lock(context.Background()); err == nil {
		t.Fatal("world-accessible parent accepted")
	}
}

func TestProductionLockerExpectedUIDIsRoot(t *testing.T) {
	locker := NewProductionLocker()
	if locker == nil || locker.ExpectedUID != 0 {
		t.Fatalf("production locker = %#v, want expected uid 0", locker)
	}
}
