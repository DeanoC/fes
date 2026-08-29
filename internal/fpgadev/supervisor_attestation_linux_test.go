//go:build linux && fpgadev

package fpgadev

import (
	"errors"
	"os"
	"testing"
)

func TestSupervisorAttestationFailsClosedWhenProcSelfExeUnavailable(t *testing.T) {
	want := errors.New("proc self unavailable")
	_, err := currentProcessAttestationWithReaders(os.Getpid(), func() (*os.File, error) {
		return nil, want
	}, func(int) ([]byte, error) {
		return nil, errors.New("stat reader must not be called")
	})
	if !errors.Is(err, want) {
		t.Fatalf("attestation error=%v, want %v", err, want)
	}
}

func TestSupervisorAttestationFailsClosedWhenProcStatUnavailable(t *testing.T) {
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	want := errors.New("proc stat unavailable")
	_, err = currentProcessAttestationWithReaders(os.Getpid(), func() (*os.File, error) {
		return self, nil
	}, func(int) ([]byte, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("attestation error=%v, want %v", err, want)
	}
}
