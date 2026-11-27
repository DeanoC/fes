package localsocket

import (
	"syscall"
	"testing"
	"time"
)

func TestConcurrentUmaskOperationsRestoreOriginal(t *testing.T) {
	original := syscall.Umask(0o022)
	defer syscall.Umask(original)
	entered := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- withUmask(0o177, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondEntered := make(chan struct{})
	second := make(chan error, 1)
	go func() {
		second <- withUmask(0o077, func() error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		close(release)
		t.Fatal("second operation entered while first umask was active")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if actual := syscall.Umask(0o022); actual != 0o022 {
		t.Fatalf("umask after concurrent operations = %04o, want 0022", actual)
	}
}
