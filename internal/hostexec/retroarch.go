package hostexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast-POC/protocol"
)

type Capability string

const HostOnly Capability = "host_only"

type State string

const (
	Idle   State = "idle"
	Active State = "active"
)

type Status struct{ State State }

type Adapter interface {
	ID() string
	Capabilities() []Capability
	Launch(context.Context, io.Reader, protocol.ContentIdentity) (Status, error)
	Stop(context.Context) error
	Status(context.Context) (Status, error)
}

type Process interface {
	Wait() error
	Kill() error
}
type NoopProcess struct{}

func (NoopProcess) Wait() error { return nil }
func (NoopProcess) Kill() error { return nil }

type StartProcess func(context.Context, string, ...string) (Process, error)

type processSession struct {
	proc Process
	path string
	done chan struct{}
}

type RetroArchAdapter struct {
	binary  string
	core    string
	start   StartProcess
	mu      sync.Mutex
	session *processSession
}

func NewRetroArchAdapter(binary, core string, start StartProcess) *RetroArchAdapter {
	if start == nil {
		start = func(_ context.Context, name string, args ...string) (Process, error) {
			cmd := exec.Command(name, args...)
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

func (a *RetroArchAdapter) Launch(_ context.Context, content io.Reader, identity protocol.ContentIdentity) (Status, error) {
	if content == nil || identity.Size < 1 || strings.TrimSpace(identity.Extension) == "" || identity.SHA256 == "" {
		return Status{}, errors.New("valid content is required")
	}
	if err := protocol.ValidateContentIdentity(identity); err != nil {
		return Status{}, err
	}
	file, err := os.CreateTemp("", "fogcast-host-*-"+filepath.Ext("."+identity.Extension))
	if err != nil {
		return Status{}, err
	}
	path := file.Name()
	cleanup := func() { _ = file.Close(); _ = os.Remove(path) }
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(content, identity.Size+1))
	if copyErr != nil || n != identity.Size || hex.EncodeToString(hash.Sum(nil)) != identity.SHA256 {
		cleanup()
		if copyErr != nil {
			return Status{}, copyErr
		}
		return Status{}, errors.New("prepared content identity mismatch")
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return Status{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return Status{}, err
	}

	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		_ = os.Remove(path)
		return Status{}, errors.New("host emulator is already active")
	}
	session := &processSession{path: path, done: make(chan struct{})}
	proc, err := a.start(context.Background(), a.binary, "-L", a.core, path)
	if err != nil {
		a.mu.Unlock()
		_ = os.Remove(path)
		return Status{}, err
	}
	session.proc = proc
	a.session = session
	a.mu.Unlock()
	go a.reap(session)
	return Status{State: Active}, nil
}

func (a *RetroArchAdapter) reap(session *processSession) {
	_ = session.proc.Wait()
	a.mu.Lock()
	if a.session == session {
		a.session = nil
	}
	a.mu.Unlock()
	_ = os.Remove(session.path)
	close(session.done)
}

func (a *RetroArchAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	session := a.session
	a.mu.Unlock()
	if session == nil {
		return nil
	}
	if err := session.proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (a *RetroArchAdapter) Status(context.Context) (Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return Status{State: Idle}, nil
	}
	return Status{State: Active}, nil
}

type commandProcess struct{ cmd *exec.Cmd }

func (p commandProcess) Wait() error { return p.cmd.Wait() }
func (p commandProcess) Kill() error {
	if p.cmd.Process == nil {
		return fmt.Errorf("process is not running")
	}
	return p.cmd.Process.Kill()
}
