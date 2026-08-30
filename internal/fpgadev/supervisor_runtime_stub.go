//go:build !linux

package fpgadev

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type SupervisorRuntimeConfig struct{}
type SupervisorRuntime struct{}

type CompatibilityMainReadiness struct{}

func NewCompatibilityMainReadiness(*SupervisorRuntime, contextProcessSnapshotter) *CompatibilityMainReadiness {
	return &CompatibilityMainReadiness{}
}

func (*CompatibilityMainReadiness) Verify(context.Context) error { return ErrUnsupported }

func mkfifoRuntimeTest(path string) error { return unix.Mkfifo(path, 0o600) }

func NewSupervisorRuntime(SupervisorRuntimeConfig) (*SupervisorRuntime, error) {
	return nil, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) StartMain(context.Context) (*ChildProcess, error) {
	return nil, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) StartAgent(context.Context) (*ChildProcess, error) {
	return nil, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) Main() *ChildProcess  { return nil }
func (*SupervisorRuntime) Agent() *ChildProcess { return nil }
func (*SupervisorRuntime) WaitChildren(context.Context) error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) CloseReceipt() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) TerminateChildren(context.Context) error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) AgentAttestation() (ProcessAttestation, error) {
	return ProcessAttestation{}, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) MainAttestation() (ProcessAttestation, error) {
	return ProcessAttestation{}, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) AgentAlive() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) ReadinessReceipt(context.Context) (ReadinessReceipt, error) {
	return ReadinessReceipt{}, errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) WaitMainReady(context.Context) error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) MainExecutableReady() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) CommandFIFOReady() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) FPGAManagerReady() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func (*SupervisorRuntime) MenuReady() error {
	return errors.New("supervisor runtime is unsupported on this platform")
}
func NewReadinessPipe() (*os.File, *os.File, error) {
	return nil, nil, errors.New("readiness pipe is unsupported on this platform")
}
func ReadReadinessReceiptFromPipe(context.Context, *os.File) (ReadinessReceipt, error) {
	return ReadinessReceipt{}, errors.New("readiness pipe is unsupported on this platform")
}
