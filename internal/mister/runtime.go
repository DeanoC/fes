package mister

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/protocol"
)

type Paths struct {
	MiSTerProcessComm string
	CommandPipe       string
	CoreNameFile      string
	MenuRBF           string
	MGLDirectory      string
}

type CommandWriter interface {
	Write(context.Context, string) error
}

type ProcessChecker interface {
	Running(string) bool
}

type Runtime struct {
	paths        Paths
	registry     core.Registry
	writer       CommandWriter
	process      ProcessChecker
	pollInterval time.Duration
}

func NewRuntime(paths Paths, registry core.Registry, writer CommandWriter, process ProcessChecker, pollInterval time.Duration) *Runtime {
	return &Runtime{paths: paths, registry: registry, writer: writer, process: process, pollInterval: pollInterval}
}

func (r *Runtime) Prepare(spec core.Spec, romPath string) (PreparedLaunch, *protocol.APIError) {
	if !r.prerequisitesReady(spec) {
		return PreparedLaunch{}, &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system prerequisite is unavailable"}
	}
	return PrepareLaunch(spec, romPath)
}

func (r *Runtime) prerequisitesReady(spec core.Spec) bool {
	if len(spec.RequiredFiles) == 0 {
		return true
	}
	for _, prerequisite := range spec.RequiredFiles {
		cleaned := filepath.Clean(filepath.FromSlash(prerequisite))
		if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			return false
		}
		info, err := os.Stat(filepath.Join(spec.MGLRoot, cleaned))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
}

type FileCommandWriter struct {
	Path string
}

func (w FileCommandWriter) Write(ctx context.Context, command string) error {
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(w.Path, os.O_WRONLY, 0)
		if err == nil {
			var written int
			written, err = f.WriteString(command)
			if err == nil && written != len(command) {
				err = io.ErrShortWrite
			}
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) readCoreName() (string, error) {
	b, err := os.ReadFile(r.paths.CoreNameFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (r *Runtime) observe(ctx context.Context, expected string) (string, *protocol.APIError) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	last := ""
	for {
		if current, err := r.readCoreName(); err == nil {
			last = current
			if current == expected {
				return current, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "expected core did not appear before the deadline"}
		case <-ticker.C:
		}
	}
}

// Launch reports whether command dispatch was attempted. Once dispatch is
// attempted, an error may be ambiguous because the command pipe can consume a
// command even when the writer reports a failure.
func (r *Runtime) Launch(ctx context.Context, prepared PreparedLaunch) (string, bool, *protocol.APIError) {
	path, err := WriteAtomicMGL(r.paths.MGLDirectory, prepared.MGL)
	if err != nil {
		return r.currentCore(), false, &protocol.APIError{Code: protocol.CodeInternal, Message: "transient MGL could not be installed"}
	}
	if err := r.writer.Write(ctx, "load_core "+path+"\n"); err != nil {
		return r.currentCore(), true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer command could not be dispatched"}
	}
	observed, apiErr := r.observe(ctx, prepared.Spec.ExpectedCore)
	return observed, true, apiErr
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	if err := r.writer.Write(ctx, "load_core "+r.paths.MenuRBF+"\n"); err != nil {
		return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Menu-core command could not be dispatched"}
	}
	return r.observe(ctx, "MENU")
}

func (r *Runtime) currentCore() string {
	name, _ := r.readCoreName()
	return name
}

func (r *Runtime) Health(version string) protocol.Health {
	process := r.process.Running(r.paths.MiSTerProcessComm)
	_, pipeErr := os.Stat(r.paths.CommandPipe)
	pipe := pipeErr == nil
	return protocol.Health{APIVersion: "v1", AgentVersion: version, Ready: process && pipe, MiSTerProcess: process, CommandPipe: pipe}
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		if r.Health("").Ready {
			if name, err := r.readCoreName(); err == nil {
				if name == "MENU" {
					return protocol.Status{State: protocol.StateIdle}
				}
				if spec, ok := r.registry.LookupObserved(name); ok {
					system, expected, observed := spec.System, spec.ExpectedCore, name
					return protocol.Status{State: protocol.StateActive, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
				}
				observed := name
				return protocol.Status{State: protocol.StateFailed, ObservedCore: &observed, LastError: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is not registered"}}
			}
		}
		select {
		case <-ctx.Done():
			return protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer dependencies did not become ready before the startup deadline"}}
		case <-ticker.C:
		}
	}
}
