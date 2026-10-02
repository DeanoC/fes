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

	"github.com/DeanoC/FogCast/internal/systems"
	"github.com/DeanoC/FogCast/protocol"
)

type Capability string

const HostOnly Capability = Capability(systems.CapabilityHostOnly)

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
	proc    Process
	path    string
	cleanup func()
	done    chan struct{}
}

type RetroArchAdapter struct {
	binary    string
	core      string
	cores     map[protocol.System]string
	start     StartProcess
	extraArgs []string
	env       []string
	sha256    map[string]string
	mu        sync.Mutex
	session   *processSession
}

type Options struct {
	Args   []string
	Env    []string
	SHA256 map[string]string
}

var ErrBusy = errors.New("host emulator is already active")
var ErrUnavailable = errors.New("host emulator core is unavailable or mismatched")

func NewRetroArchAdapter(binary, core string, start StartProcess) *RetroArchAdapter {
	return NewRetroArchAdapterWithCores(binary, core, nil, start)
}

func NewRetroArchAdapterWithCores(binary, defaultCore string, cores map[protocol.System]string, start StartProcess) *RetroArchAdapter {
	return NewRetroArchAdapterConfigured(binary, defaultCore, cores, Options{}, start)
}

func NewRetroArchAdapterConfigured(binary, defaultCore string, cores map[protocol.System]string, options Options, start StartProcess) *RetroArchAdapter {
	if start == nil {
		start = func(_ context.Context, name string, args ...string) (Process, error) {
			cmd := exec.Command(name, args...)
			if len(options.Env) > 0 {
				cmd.Env = append(os.Environ(), options.Env...)
			}
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return commandProcess{cmd}, nil
		}
	}
	copied := make(map[protocol.System]string, len(cores))
	for system, core := range cores {
		copied[system] = core
	}
	pins := make(map[string]string, len(options.SHA256))
	for path, sum := range options.SHA256 {
		pins[path] = strings.ToLower(sum)
	}
	return &RetroArchAdapter{binary: binary, core: defaultCore, cores: copied, start: start, extraArgs: append([]string(nil), options.Args...), env: append([]string(nil), options.Env...), sha256: pins}
}

func (a *RetroArchAdapter) coreFor(system protocol.System) string {
	if core := a.cores[system]; core != "" {
		return core
	}
	return a.core
}

func (a *RetroArchAdapter) ID() string                 { return "retroarch" }
func (a *RetroArchAdapter) Capabilities() []Capability { return []Capability{HostOnly} }

func (a *RetroArchAdapter) Launch(ctx context.Context, content io.Reader, identity protocol.ContentIdentity) (Status, error) {
	return a.launch(ctx, a.core, content, identity, "", nil)
}

func (a *RetroArchAdapter) LaunchFor(ctx context.Context, system protocol.System, content io.Reader, identity protocol.ContentIdentity) (Status, error) {
	core := a.coreFor(system)
	if core == "" {
		return Status{}, fmt.Errorf("%w: no core configured for system %q", ErrUnavailable, system)
	}
	return a.launch(ctx, core, content, identity, "", nil)
}

func (a *RetroArchAdapter) LaunchPath(ctx context.Context, system protocol.System, sourcePath string) (Status, error) {
	return a.launchOwnedPath(ctx, system, sourcePath, nil)
}

func (a *RetroArchAdapter) LaunchOwnedPath(ctx context.Context, system protocol.System, sourcePath string, cleanup func()) (Status, error) {
	return a.launchOwnedPath(ctx, system, sourcePath, cleanup)
}

func (a *RetroArchAdapter) launchOwnedPath(ctx context.Context, system protocol.System, sourcePath string, cleanup func()) (Status, error) {
	core := a.coreFor(system)
	if core == "" {
		if cleanup != nil {
			cleanup()
		}
		return Status{}, fmt.Errorf("%w: no core configured for system %q", ErrUnavailable, system)
	}
	if strings.TrimSpace(sourcePath) == "" {
		if cleanup != nil {
			cleanup()
		}
		return Status{}, errors.New("host emulator source is not configured")
	}
	return a.launch(ctx, core, nil, protocol.ContentIdentity{}, sourcePath, cleanup)
}

func (a *RetroArchAdapter) launch(ctx context.Context, core string, content io.Reader, identity protocol.ContentIdentity, sourcePath string, ownedCleanup func()) (Status, error) {
	info, statErr := os.Stat(core)
	if statErr != nil || !info.Mode().IsRegular() {
		return Status{}, fmt.Errorf("%w: core file missing", ErrUnavailable)
	}
	if expected := a.sha256[core]; expected != "" {
		file, openErr := os.Open(core)
		if openErr != nil {
			return Status{}, fmt.Errorf("%w: core file missing", ErrUnavailable)
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return Status{}, fmt.Errorf("%w: sha256 mismatch", ErrUnavailable)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	path := strings.TrimSpace(sourcePath)
	cleanup := ownedCleanup
	if cleanup == nil {
		cleanup = func() {}
	}
	keepLibraryPath := path != "" && ownedCleanup == nil
	if path == "" {
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
		path = file.Name()
		cleanup = func() { _ = file.Close(); _ = os.Remove(path) }
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(file, hash), contextReader{ctx: ctx, r: io.LimitReader(content, identity.Size+1)})
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
		cleanup = func() { _ = os.Remove(path) }
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return Status{}, err
	}

	a.mu.Lock()
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		cleanup()
		return Status{}, err
	}
	if a.session != nil {
		a.mu.Unlock()
		cleanup()
		return Status{}, ErrBusy
	}
	session := &processSession{done: make(chan struct{})}
	switch {
	case keepLibraryPath:
	case ownedCleanup != nil:
		session.cleanup = ownedCleanup
	default:
		session.path = path
	}
	args := append([]string(nil), a.extraArgs...)
	args = append(args, "-L", core, path)
	proc, err := a.start(context.Background(), a.binary, args...)
	if err != nil {
		a.mu.Unlock()
		cleanup()
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
	if session.cleanup != nil {
		session.cleanup()
	} else if session.path != "" {
		_ = os.Remove(session.path)
	}
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

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
