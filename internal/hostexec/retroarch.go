package hostexec

import (
	"context"
	"errors"
	"os/exec"
	"sync"
)

type Capability string

const HostOnly Capability = "host_only"

type State string

const (
	Idle   State = "idle"
	Active State = "active"
)

type Status struct {
	State State
}

type Process interface {
	Wait() error
	Kill() error
}

type NoopProcess struct{}

func (NoopProcess) Wait() error { return nil }
func (NoopProcess) Kill() error { return nil }

type StartProcess func(context.Context, string, ...string) (Process, error)

type RetroArchAdapter struct {
	binary string
	core   string
	start  StartProcess
	mu     sync.Mutex
	proc   Process
}

func NewRetroArchAdapter(binary, core string, start StartProcess) *RetroArchAdapter {
	if start == nil {
		start = func(ctx context.Context, name string, args ...string) (Process, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return commandProcess{cmd}, nil
		}
	}
	return &RetroArchAdapter{binary: binary, core: core, start: start}
}

func (a *RetroArchAdapter) ID() string                 { return "retroarch" }
func (a *RetroArchAdapter) Capabilities() []Capability { return []Capability{HostOnly} }

func (a *RetroArchAdapter) Launch(ctx context.Context, contentPath string) (Status, error) {
	if contentPath == "" {
		return Status{}, errors.New("content path is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc != nil {
		return Status{}, errors.New("host emulator is already active")
	}
	proc, err := a.start(ctx, a.binary, "-L", a.core, contentPath)
	if err != nil {
		return Status{}, err
	}
	a.proc = proc
	return Status{State: Active}, nil
}

func (a *RetroArchAdapter) Stop(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc == nil {
		return nil
	}
	err := a.proc.Kill()
	a.proc = nil
	return err
}

func (a *RetroArchAdapter) Status(context.Context) (Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc == nil {
		return Status{State: Idle}, nil
	}
	return Status{State: Active}, nil
}

type commandProcess struct{ cmd *exec.Cmd }

func (p commandProcess) Wait() error { return p.cmd.Wait() }
func (p commandProcess) Kill() error { return p.cmd.Process.Kill() }
