//go:build !linux || !arm || !fpgadev

package fpgadev

import (
	"context"
	"errors"
	"os"
)

type ChildProcess struct {
	waitFn func(context.Context) error
	termFn func(context.Context) error
	attFn  func() (ProcessAttestation, error)
}

func newFakeChild() *ChildProcess { return &ChildProcess{} }

// Non-Linux, non-ARM, and untagged builds expose the typed child API but never
// construct a process. Production dependency construction is ARM-gated in
// cmd/fogcast-dev-supervisor; amd64 tests use injected supervisor fakes.
func StartChildProcess(context.Context, string, []string, []string, []*os.File) (*ChildProcess, error) {
	return nil, errors.New("child processes are unsupported on this platform")
}
func (*ChildProcess) PID() int { return 0 }
func (p *ChildProcess) Wait(ctx context.Context) error {
	if p != nil && p.waitFn != nil {
		return p.waitFn(ctx)
	}
	return errors.New("child processes are unsupported on this platform")
}
func (p *ChildProcess) TerminateAndReap(ctx context.Context) error {
	if p != nil && p.termFn != nil {
		return p.termFn(ctx)
	}
	return errors.New("child processes are unsupported on this platform")
}
func (p *ChildProcess) Attestation() (ProcessAttestation, error) {
	if p != nil && p.attFn != nil {
		return p.attFn()
	}
	return ProcessAttestation{}, errors.New("child processes are unsupported on this platform")
}
func (p *ChildProcess) StartAttestation() (ProcessAttestation, error) {
	if p != nil && p.attFn != nil {
		return p.attFn()
	}
	return ProcessAttestation{}, errors.New("child processes are unsupported on this platform")
}
func (p *ChildProcess) Alive() error {
	if p != nil && p.attFn != nil {
		_, err := p.attFn()
		return err
	}
	return errors.New("child processes are unsupported on this platform")
}
