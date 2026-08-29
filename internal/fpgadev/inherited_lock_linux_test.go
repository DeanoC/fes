//go:build linux && fpgadev

package fpgadev

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"golang.org/x/sys/unix"
)

func TestInheritedInstallLockRejectsStandardDescriptors(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "install.lock")
	for _, fd := range []int{0, 1, 2} {
		if _, err := retainInheritedInstallLockFD(fd, path); err == nil {
			t.Fatalf("standard descriptor %d was accepted", fd)
		}
	}
}

func TestInheritedInstallLockRetainsHeldFileWithoutReacquiring(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "install.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fdOwned := true
	defer func() {
		if fdOwned {
			_ = unix.Close(fd)
		}
	}()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	retained, err := retainInheritedInstallLockFD(fd, path)
	if err != nil {
		t.Fatal(err)
	}
	if retained == nil || int(retained.Fd()) != fd {
		t.Fatalf("retained file=%v fd=%d, want exact fd %d", retained, retained.Fd(), fd)
	}
	locker := hardwareowner.NewLocker(path, uint32(os.Getuid()))
	contenderCtx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, contenderErr := locker.Lock(contenderCtx)
	if contenderErr == nil || !errors.Is(contenderErr, context.DeadlineExceeded) {
		t.Fatalf("separate contender result=%v, want bounded blocking deadline", contenderErr)
	}
	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	fdOwned = false
}

func TestInheritedInstallLockRejectsUnheldDescriptor(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "install.lock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	candidateFD, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(candidateFD)
	if _, err := unix.FcntlInt(uintptr(candidateFD), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	if retained, err := retainInheritedInstallLockFD(candidateFD, path); err == nil {
		if retained != nil {
			_ = retained.Close()
		}
		t.Fatal("unheld descriptor was accepted")
	}
}

func TestRejectedInheritedFDHasNoLatentGoCloserAfterReuse(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "install.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(lockPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	if _, err := retainInheritedInstallLockFD(fd, lockPath); err == nil {
		_ = unix.Close(fd)
		t.Fatal("unheld descriptor was accepted")
	}
	if err := unix.Close(fd); err != nil {
		t.Fatal(err)
	}
	runtime.GC()

	tempPath := filepath.Join(root, "reused.tmp")
	reused, err := unix.Open(tempPath, unix.O_RDWR|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if reused != fd {
		if err := unix.Dup2(reused, fd); err != nil {
			_ = unix.Close(reused)
			t.Fatal(err)
		}
		_ = unix.Close(reused)
	}
	runtime.GC()
	if err := unix.Fsync(fd); err != nil {
		_ = unix.Close(fd)
		t.Fatalf("reused descriptor fsync: %v", err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatalf("reused descriptor close: %v", err)
	}
}

type countingInstallLocker struct {
	calls int
}

func (l *countingInstallLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	l.calls++
	return nil, errors.New("unexpected install-lock reacquire")
}

func TestSupervisorUsesRetainedInheritedLockWithoutReacquiring(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "install.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fdOwned := true
	defer func() {
		if fdOwned {
			_ = unix.Close(fd)
		}
	}()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	counting := &countingInstallLocker{}
	deps := Dependencies{InheritedLockFD: fd, InheritedLockFDSet: true, InstallLocker: counting}
	retained, release, err := acquireSupervisorInstallLock(context.Background(), deps, path)
	if err != nil {
		t.Fatal(err)
	}
	if retained == nil || int(retained.Fd()) != fd {
		_ = release()
		t.Fatalf("retained fd=%v, want %d", retained, fd)
	}
	if counting.calls != 0 {
		_ = release()
		t.Fatalf("InstallLocker.Lock calls=%d, want zero", counting.calls)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	fdOwned = false
}

type orderedInstallLocker struct {
	events *[]string
}

func (l *orderedInstallLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	*l.events = append(*l.events, "install-lock")
	return func() error {
		*l.events = append(*l.events, "install-unlock")
		return nil
	}, nil
}

type orderedOwnerLocker struct {
	events *[]string
}

func (l *orderedOwnerLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	*l.events = append(*l.events, "owner-lock")
	return func() error {
		*l.events = append(*l.events, "owner-unlock")
		return nil
	}, nil
}

func TestSupervisorAdmissionReleaseAndReacquireOrder(t *testing.T) {
	events := []string{}
	deps := Dependencies{
		InstallLocker: &orderedInstallLocker{events: &events},
		OwnerLocker:   &orderedOwnerLocker{events: &events},
	}
	installUnlock, ownerUnlock, err := reacquireSupervisorAdmission(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseSupervisorAdmission(ownerUnlock, installUnlock); err != nil {
		t.Fatal(err)
	}
	want := []string{"install-lock", "owner-lock", "owner-unlock", "install-unlock"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("admission events=%v, want %v", events, want)
	}
}
