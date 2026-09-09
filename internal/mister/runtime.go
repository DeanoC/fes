package mister

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/protocol"
)

type Paths struct {
	MiSTerProcessComm string
	CommandPipe       string
	CoreNameFile      string
	BootIDFile        string
	MenuRBF           string
	MGLDirectory      string
	DevelopmentRBF    string
	RebootCommand     string
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
	events       flightdiag.Sink
	processMu    sync.Mutex
	processKnown bool
	processState bool
}

func NewRuntime(paths Paths, registry core.Registry, writer CommandWriter, process ProcessChecker, pollInterval time.Duration, options ...RuntimeOption) *Runtime {
	runtime := &Runtime{paths: paths, registry: registry, writer: writer, process: process, pollInterval: pollInterval}
	for _, option := range options {
		if option != nil {
			option(runtime)
		}
	}
	return runtime
}

type RuntimeOption func(*Runtime)

func WithEventSink(sink flightdiag.Sink) RuntimeOption {
	return func(runtime *Runtime) { runtime.ConfigureDiagnostics(sink) }
}

func (r *Runtime) ConfigureDiagnostics(sink flightdiag.Sink) {
	r.events = sink
	if writer, ok := r.writer.(interface{ ConfigureDiagnostics(flightdiag.Sink) }); ok {
		writer.ConfigureDiagnostics(sink)
	}
}

func (r *Runtime) record(kind, severity string, detail map[string]any) {
	if r.events == nil {
		return
	}
	r.events.Append(flightdiag.Event{
		Layer:    flightdiag.LayerRuntime,
		Kind:     kind,
		Severity: severity,
		Detail:   detail,
	})
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
	Path   string
	events flightdiag.Sink
}

func (w *FileCommandWriter) ConfigureDiagnostics(sink flightdiag.Sink) {
	w.events = sink
}

func (w FileCommandWriter) Write(ctx context.Context, command string) error {
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(w.Path, os.O_WRONLY, 0)
		if err != nil {
			if w.events != nil {
				w.events.Append(flightdiag.Event{
					Layer:    flightdiag.LayerRuntime,
					Kind:     flightdiag.KindCapFDOpen,
					Severity: "error",
					Detail:   map[string]any{"ok": false},
				})
			}
		} else if w.events != nil {
			w.events.Append(flightdiag.Event{
				Layer:    flightdiag.LayerRuntime,
				Kind:     flightdiag.KindCapFDOpen,
				Severity: "ok",
				Detail:   map[string]any{"ok": true},
			})
		}
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
			if current != last {
				r.record(flightdiag.KindCoreNameChange, "ok", map[string]any{"observed": current, "expected": expected})
			}
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
		r.record(flightdiag.KindFIFODispatch, "error", map[string]any{"operation": "launch", "ok": false})
		return r.currentCore(), true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer command could not be dispatched"}
	}
	r.record(flightdiag.KindFIFODispatch, "ok", map[string]any{"operation": "launch", "ok": true})
	observed, apiErr := r.observe(ctx, prepared.Spec.ExpectedCore)
	return observed, true, apiErr
}

func (r *Runtime) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	if err := WriteAtomicDevelopmentRBF(r.paths.DevelopmentRBF, size, content); err != nil {
		return r.currentCore(), false, &protocol.APIError{Code: protocol.CodeInternal, Message: "development RBF could not be installed"}
	}
	if err := r.writer.Write(ctx, "load_core "+r.paths.DevelopmentRBF+"\n"); err != nil {
		r.record(flightdiag.KindFIFODispatch, "error", map[string]any{"operation": "development_rbf", "ok": false})
		return r.currentCore(), true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "MiSTer command could not be dispatched"}
	}
	r.record(flightdiag.KindFIFODispatch, "ok", map[string]any{"operation": "development_rbf", "ok": true})
	return r.currentCore(), true, nil
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	if err := r.writer.Write(ctx, "load_core "+r.paths.MenuRBF+"\n"); err != nil {
		r.record(flightdiag.KindFIFODispatch, "error", map[string]any{"operation": "stop", "ok": false})
		return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "Menu-core command could not be dispatched"}
	}
	r.record(flightdiag.KindFIFODispatch, "ok", map[string]any{"operation": "stop", "ok": true})
	return r.observe(ctx, "MENU")
}

func (r *Runtime) RecoverDevelopment(ctx context.Context) (string, *protocol.APIError) {
	r.record(flightdiag.KindFenceRecovery, "warn", map[string]any{"operation": "development_reboot"})
	if err := ctx.Err(); err != nil {
		return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "development-core recovery was cancelled"}
	}
	info, err := os.Stat(r.paths.RebootCommand)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "development-core reboot recovery is unavailable"}
	}
	command := exec.Command(r.paths.RebootCommand)
	if err := command.Start(); err != nil {
		return r.currentCore(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "development-core reboot could not be started"}
	}
	go func() { _ = command.Wait() }()
	return r.currentCore(), nil
}

func (r *Runtime) currentCore() string {
	name, _ := r.readCoreName()
	return name
}

func (r *Runtime) Health(version string) protocol.Health {
	process := r.process.Running(r.paths.MiSTerProcessComm)
	r.observeProcess(process)
	_, pipeErr := os.Stat(r.paths.CommandPipe)
	pipe := pipeErr == nil
	bootID, _ := os.ReadFile(r.paths.BootIDFile)
	return protocol.Health{APIVersion: "v1", AgentVersion: version, Ready: process && pipe, MiSTerProcess: process, CommandPipe: pipe, BootID: strings.TrimSpace(string(bootID))}
}

func (r *Runtime) observeProcess(running bool) {
	r.processMu.Lock()
	defer r.processMu.Unlock()
	if !r.processKnown {
		r.processKnown = true
		r.processState = running
		if running {
			r.record(flightdiag.KindMainStart, "ok", map[string]any{"observed": true})
		}
		return
	}
	if running == r.processState {
		return
	}
	r.processState = running
	if running {
		r.record(flightdiag.KindMainAppRestart, "warn", map[string]any{"observed": true})
	} else {
		r.record(flightdiag.KindMainExit, "warn", map[string]any{"observed": false})
	}
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
