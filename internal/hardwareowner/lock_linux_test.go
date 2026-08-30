//go:build linux

package hardwareowner

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestLockerOpensExistingProtectedLockReadOnly catches reopening an existing
// lock with O_RDWR|O_CREAT. That fails on the target's normally read-only root
// even though exclusive flock itself works on a read-only descriptor.
func TestLockerOpensExistingProtectedLockReadOnly(t *testing.T) {
	locker := testLocker(t)
	if err := os.WriteFile(locker.Path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	file, unlock, err := locker.LockFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := flags & unix.O_ACCMODE; got != unix.O_RDONLY {
		t.Fatalf("existing lock access mode = %#x, want O_RDONLY", got)
	}
}

func TestLockerRejectsExistingFIFOWithoutBlocking(t *testing.T) {
	locker := testLocker(t)
	if err := unix.Mkfifo(locker.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := locker.Lock(context.Background())
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("FIFO lock entry was accepted")
		}
	case <-time.After(100 * time.Millisecond):
		writer, err := unix.Open(locker.Path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			_ = unix.Close(writer)
		}
		<-result
		t.Fatal("FIFO lock entry blocked during open")
	}
}
