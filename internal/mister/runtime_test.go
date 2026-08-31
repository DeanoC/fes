package mister_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/protocol"
)

type fakeWriter struct {
	mu       sync.Mutex
	commands []string
	onWrite  func(string)
}

func (f *fakeWriter) Write(_ context.Context, command string) error {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	f.mu.Unlock()
	if f.onWrite != nil {
		f.onWrite(command)
	}
	return nil
}

func (f *fakeWriter) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.commands)
}

type fixedProcess bool

func (f fixedProcess) Running(string) bool {
	return bool(f)
}

func TestRuntimeLaunchAndStop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	coreName := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	if err := os.WriteFile(commandPipe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writer := &fakeWriter{}
	registry := core.DefaultRegistry()
	mglDirectory := filepath.Join(dir, "mgl")
	runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreName, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: mglDirectory}, registry, writer, fixedProcess(true), 5*time.Millisecond)
	spec, _ := registry.Lookup(protocol.SystemMegaDrive)
	romRoot := filepath.Join(dir, "games", "MegaDrive")
	if err := os.MkdirAll(romRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(romRoot, "test.md")
	if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec.ROMRoot = romRoot
	prepared, apiErr := runtime.Prepare(spec, rom)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	writer.onWrite = func(command string) {
		name := "MegaDrive"
		if command == "load_core /media/fat/menu.rbf\n" {
			name = "MENU"
		}
		if err := os.WriteFile(coreName, []byte(name+"\n"), 0o600); err != nil {
			panic(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	observed, dispatched, apiErr := runtime.Launch(ctx, prepared)
	if apiErr != nil || observed != "MegaDrive" || !dispatched {
		t.Fatalf("launch = %q, %t, %#v", observed, dispatched, apiErr)
	}
	observed, apiErr = runtime.Stop(ctx)
	if apiErr != nil || observed != "MENU" {
		t.Fatalf("stop = %q, %#v", observed, apiErr)
	}
	wantCommands := []string{
		"load_core " + filepath.Join(mglDirectory, "launch.mgl") + "\n",
		"load_core /media/fat/menu.rbf\n",
	}
	if got := writer.snapshot(); !slices.Equal(got, wantCommands) {
		t.Fatalf("commands = %q, want %q", got, wantCommands)
	}
}

func TestRuntimeLaunchTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	coreName := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	if err := os.WriteFile(commandPipe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coreName, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreName, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond)
	prepared := mister.PreparedLaunch{Spec: core.Spec{ExpectedCore: "SNES"}, MGL: []byte("mgl\n")}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	observed, dispatched, apiErr := runtime.Launch(ctx, prepared)
	if apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout || observed != "MENU" || !dispatched {
		t.Fatalf("launch timeout = %q, %t, %#v", observed, dispatched, apiErr)
	}
}

func TestRuntimeLaunchReportsFailureBeforeDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mglDirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(mglDirectory, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := &fakeWriter{}
	runtime := mister.NewRuntime(mister.Paths{CoreNameFile: filepath.Join(dir, "CORENAME"), MGLDirectory: mglDirectory}, core.DefaultRegistry(), writer, fixedProcess(true), time.Millisecond)

	_, dispatched, apiErr := runtime.Launch(context.Background(), mister.PreparedLaunch{Spec: core.Spec{ExpectedCore: "SNES"}, MGL: []byte("mgl\n")})
	if apiErr == nil || apiErr.Code != protocol.CodeInternal || dispatched {
		t.Fatalf("launch = dispatched %t, error %#v", dispatched, apiErr)
	}
	if commands := writer.snapshot(); len(commands) != 0 {
		t.Fatalf("commands = %q; want none", commands)
	}
}

func TestRuntimeHealthRequiresProcessAndPipe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pipe := filepath.Join(dir, "MiSTer_cmd")
	paths := mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: pipe, CoreNameFile: filepath.Join(dir, "CORENAME"), MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}
	withoutPipe := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond).Health("0.1.0")
	if withoutPipe.Ready || !withoutPipe.MiSTerProcess || withoutPipe.CommandPipe {
		t.Fatalf("health without pipe = %#v", withoutPipe)
	}
	if err := os.WriteFile(pipe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	withoutProcess := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(false), time.Millisecond).Health("0.1.0")
	if withoutProcess.Ready || withoutProcess.MiSTerProcess || !withoutProcess.CommandPipe {
		t.Fatalf("health without process = %#v", withoutProcess)
	}
	ready := mister.NewRuntime(paths, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond).Health("0.1.0")
	if !ready.Ready || ready.AgentVersion != "0.1.0" || ready.APIVersion != "v1" {
		t.Fatalf("ready health = %#v", ready)
	}
}

func TestRuntimePrepareRequiresNonEmptyLynxBootROM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	registry := core.DefaultRegistry()
	spec, ok := registry.Lookup(protocol.SystemAtariLynx)
	if !ok {
		t.Fatal("Lynx core spec is missing")
	}
	romRoot := filepath.Join(dir, "cache")
	dataRoot := filepath.Join(dir, "games", "AtariLynx")
	if err := os.MkdirAll(romRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(romRoot, "test.lnx")
	if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec.ROMRoot = romRoot
	spec.MGLRoot = dataRoot
	runtime := mister.NewRuntime(mister.Paths{MenuRBF: filepath.Join(dir, "opt", "fogcast", "menu.rbf")}, registry, &fakeWriter{}, fixedProcess(true), time.Millisecond)

	if _, apiErr := runtime.Prepare(spec, rom); apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
		t.Fatalf("Prepare without boot.rom error = %#v", apiErr)
	}
	bootROM := filepath.Join(dataRoot, "boot.rom")
	if err := os.WriteFile(bootROM, []byte("boot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, apiErr := runtime.Prepare(spec, rom); apiErr != nil {
		t.Fatalf("Prepare with boot.rom: %v", apiErr)
	}
}

func TestReconcileCoreNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		coreName string
		create   bool
		state    protocol.State
		system   *protocol.System
		code     protocol.ErrorCode
	}{
		{name: "menu", coreName: "MENU", create: true, state: protocol.StateIdle},
		{name: "registered", coreName: "SNES", create: true, state: protocol.StateActive, system: func() *protocol.System { v := protocol.SystemSNES; return &v }()},
		{name: "shared SMS fallback", coreName: "SMS", create: true, state: protocol.StateActive, system: func() *protocol.System { v := protocol.SystemSMS; return &v }()},
		{name: "unknown", coreName: "UNKNOWN", create: true, state: protocol.StateFailed, code: protocol.CodeUnrecognizedCore},
		{name: "missing", create: false, state: protocol.StateFailed, code: protocol.CodeMiSTerUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			coreNameFile := filepath.Join(dir, "CORENAME")
			commandPipe := filepath.Join(dir, "MiSTer_cmd")
			if err := os.WriteFile(commandPipe, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.create {
				if err := os.WriteFile(coreNameFile, []byte(tt.coreName+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runtime := mister.NewRuntime(mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreNameFile, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}, core.DefaultRegistry(), &fakeWriter{}, fixedProcess(true), time.Millisecond)
			ctx := context.Background()
			if !tt.create {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}
			status := runtime.Reconcile(ctx)
			if status.State != tt.state || status.GameID != nil {
				t.Fatalf("status = %#v", status)
			}
			if tt.system != nil && (status.System == nil || *status.System != *tt.system) {
				t.Fatalf("system = %#v", status.System)
			}
			if tt.code != "" && (status.LastError == nil || status.LastError.Code != tt.code) {
				t.Fatalf("last error = %#v", status.LastError)
			}
		})
	}
}

func TestFileCommandWriterWritesCompleteCommand(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "MiSTer_cmd")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (mister.FileCommandWriter{Path: path}).Write(context.Background(), "load_core /tmp/launch.mgl\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "load_core /tmp/launch.mgl\n" {
		t.Fatalf("command = %q", got)
	}
}
