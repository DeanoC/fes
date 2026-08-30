//go:build linux && amd64 && fpgadev

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisorRuntimeHostUsesInjectedTypedChildStarter(t *testing.T) {
	started := 0
	starter := func(_ context.Context, _ string, _ []string, _ []string, _ []*os.File) (*ChildProcess, error) {
		started++
		return newFakeChild(), nil
	}
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/tmp/main", MainFIFO: "/tmp/fifo", FPGAManagerState: "/tmp/state",
		MenuPath: "/tmp/menu", CoreNameFile: "/tmp/core", AgentExecutable: "/tmp/agent",
		StartChild: starter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if started != 2 {
		t.Fatalf("starter calls = %d, want 2", started)
	}
}

func TestCompatibilityMainReadinessAlreadyMENUAtTmpWhenMediaFatAbsent(t *testing.T) {
	dir := t.TempDir()
	mediaFatCore := filepath.Join(dir, "media", "fat", "CORENAME")
	tmpCore := filepath.Join(dir, "tmp", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(tmpCore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpCore, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, mediaFatCore, tmpCore)
	if runtime.main != nil {
		t.Fatal("fresh runtime unexpectedly owns Main")
	}
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityMainReadinessAcceptsFourByteMENUAtTmpWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	mediaFatCore := filepath.Join(dir, "media", "fat", "CORENAME")
	tmpCore := filepath.Join(dir, "tmp", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(tmpCore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpCore, []byte("MENU"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, mediaFatCore, tmpCore)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityMainReadinessAlreadyMENUAtMediaFatWhenPresent(t *testing.T) {
	dir := t.TempDir()
	tmpCore := filepath.Join(dir, "tmp", "CORENAME")
	mediaFatCore := filepath.Join(dir, "media", "fat", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(mediaFatCore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaFatCore, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, tmpCore, mediaFatCore)
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRuntimePairsOnlyCanonicalCORENAMEPaths(t *testing.T) {
	for _, test := range []struct {
		name     string
		primary  string
		fallback string
	}{
		{name: "tmp primary", primary: "/tmp/CORENAME", fallback: "/media/fat/CORENAME"},
		{name: "media fat primary", primary: "/media/fat/CORENAME", fallback: "/tmp/CORENAME"},
		{name: "custom primary", primary: "/fixture/CORENAME"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{CoreNameFile: test.primary})
			if err != nil {
				t.Fatal(err)
			}
			if runtime.config.CoreNameFallbackFile != test.fallback {
				t.Fatalf("fallback CORENAME path=%q, want %q", runtime.config.CoreNameFallbackFile, test.fallback)
			}
		})
	}
}

func TestCompatibilityMainReadinessFailsClosedForNonMENUCORENAME(t *testing.T) {
	for _, test := range []struct {
		name  string
		first string
		other string
	}{
		{name: "wrong core", first: "MegaDrive\n"},
		{name: "missing both paths"},
		{name: "garbage CORENAME", first: "MENU\x00\n"},
		{name: "conflicting publishers", first: "MENU\n", other: "NES\n"},
		{name: "lowercase menu", first: "menu"},
		{name: "trailing space", first: "MENU "},
		{name: "trailing data", first: "MENU\nX"},
		{name: "second trailing newline", first: "MENU\n\n"},
		{name: "carriage return", first: "MENU\r"},
		{name: "CRLF", first: "MENU\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			first := filepath.Join(dir, "tmp", "CORENAME")
			other := filepath.Join(dir, "media", "fat", "CORENAME")
			for path, data := range map[string]string{first: test.first, other: test.other} {
				if data == "" {
					continue
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runtime, observer := newCompatibilityMainReadinessFixture(t, first, other)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Verify error=%v, want fail-closed deadline", err)
			}
		})
	}
}

func TestCompatibilityMainReadinessFailsClosedForSymlinkCORENAME(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "tmp", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(core), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, core); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "media", "fat", "CORENAME"))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify error=%v, want fail-closed deadline", err)
	}
}

func TestCompatibilityMainReadinessMalformedFallbackCannotEscapeDeadline(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "tmp", "CORENAME")
	fallback := filepath.Join(dir, "media", "fat", "CORENAME")
	for _, path := range []string{primary, fallback} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(primary, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mkfifoRuntimeTest(fallback); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, primary, fallback)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify error=%v, want fail-closed deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("malformed fallback escaped readiness deadline: %v", elapsed)
	}
}

func TestCompatibilityMainReadinessKeepsFIFOAndFPGAManagerGates(t *testing.T) {
	for _, test := range []struct {
		name      string
		breakGate func(*SupervisorRuntime) error
	}{
		{
			name: "command FIFO",
			breakGate: func(runtime *SupervisorRuntime) error {
				return os.Remove(runtime.config.MainFIFO)
			},
		},
		{
			name: "FPGA manager",
			breakGate: func(runtime *SupervisorRuntime) error {
				return os.WriteFile(runtime.config.FPGAManagerState, []byte("unknown\n"), 0o600)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			core := filepath.Join(dir, "tmp", "CORENAME")
			if err := os.MkdirAll(filepath.Dir(core), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(core, []byte("MENU\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, observer := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "media", "fat", "CORENAME"))
			if err := test.breakGate(runtime); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Verify error=%v, want fail-closed deadline", err)
			}
		})
	}
}

func newCompatibilityMainReadinessFixture(t *testing.T, coreNameFile, fallbackCoreNameFile string) (*SupervisorRuntime, *Observer) {
	t.Helper()
	dir := t.TempDir()
	fifo, state, menu := filepath.Join(dir, "cmd"), filepath.Join(dir, "state"), filepath.Join(dir, "menu")
	if err := mkfifoRuntimeTest(fifo); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{state: "operating\n", menu: "menu"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	expected := ExecutableIdentity{Device: 8, Inode: 9, SHA256: strings.Repeat("a", 64)}
	observer := &Observer{Expected: expected, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{{Identity: ProcessIdentity{PID: 41, StartTime: 7, Device: 8, Inode: 9, SHA256: expected.SHA256}}}}}}
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/tmp/main", MainFIFO: fifo, FPGAManagerState: state,
		MenuPath: menu, CoreNameFile: coreNameFile, CoreNameFallbackFile: fallbackCoreNameFile,
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, observer
}

func TestSupervisorRuntimeHostReadinessPipeHonorsDeadline(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ReadReadinessReceiptFromPipe(ctx, rd); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestSupervisorRuntimeHostReceiptReadinessAndExactCleanup(t *testing.T) {
	dir := t.TempDir()
	fifo, state, menu, core := filepath.Join(dir, "cmd"), filepath.Join(dir, "state"), filepath.Join(dir, "menu"), filepath.Join(dir, "CORENAME")
	if err := mkfifoRuntimeTest(fifo); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{state: "operating\n", menu: "menu", core: "MENU\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	profile := sha256.Sum256([]byte("profile"))
	identity := ProcessAttestation{PID: 41, StartTime: 7, Device: 8, Inode: 9, SHA256: hex.EncodeToString(profile[:])}
	agentIdentity := ProcessAttestation{PID: 42, StartTime: 8, Device: 8, Inode: 10, SHA256: hex.EncodeToString(profile[:])}
	terminated := 0
	starter := func(_ context.Context, _ string, args, _ []string, files []*os.File) (*ChildProcess, error) {
		child := &ChildProcess{attFn: func() (ProcessAttestation, error) { return identity, nil }, termFn: func(context.Context) error { terminated++; return nil }}
		if len(files) != 0 {
			child.attFn = func() (ProcessAttestation, error) { return agentIdentity, nil }
			rcpt := ReadinessReceipt{Schema: 1, PID: agentIdentity.PID, StartTime: agentIdentity.StartTime, ExecutableDevice: agentIdentity.Device, ExecutableInode: agentIdentity.Inode, ExecutableSHA256: agentIdentity.SHA256, ProfileSHA256: hex.EncodeToString(profile[:]), Capabilities: DevelopmentCapabilities()}
			raw, err := rcpt.MarshalCanonical()
			if err != nil {
				return nil, err
			}
			if len(args) == 0 {
				return nil, errors.New("agent args missing")
			}
			if _, err := files[0].Write(raw); err != nil {
				return nil, err
			}
		}
		return child, nil
	}
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainExecutable: "/tmp/main", MainFIFO: fifo, FPGAManagerState: state, MenuPath: menu, CoreNameFile: core, AgentExecutable: "/tmp/agent", StartChild: starter, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.WaitMainReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := r.ReadinessReceipt(context.Background()); err != nil || got.PID != agentIdentity.PID {
		t.Fatalf("receipt=%+v err=%v", got, err)
	}
	if err := r.TerminateChildren(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terminated != 2 {
		t.Fatalf("terminated handles = %d, want 2", terminated)
	}
}

func TestSupervisorRuntimeHostReceiptFramingFailuresReapExactAgent(t *testing.T) {
	for _, mode := range []string{"empty", "partial", "extra", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			terminated := 0
			identity := ProcessAttestation{PID: 51, StartTime: 9, Device: 8, Inode: 11, SHA256: strings.Repeat("a", 64)}
			starter := func(_ context.Context, _ string, _ []string, _ []string, files []*os.File) (*ChildProcess, error) {
				child := &ChildProcess{attFn: func() (ProcessAttestation, error) { return identity, nil }, termFn: func(context.Context) error { terminated++; return nil }}
				if len(files) != 0 {
					raw := []byte{}
					if mode == "oversize" {
						raw = []byte(strings.Repeat("x", ReadinessReceiptMaxBytes+1))
					} else {
						r := ReadinessReceipt{Schema: 1, PID: identity.PID, StartTime: identity.StartTime, ExecutableDevice: identity.Device, ExecutableInode: identity.Inode, ExecutableSHA256: identity.SHA256, ProfileSHA256: strings.Repeat("b", 64), Capabilities: DevelopmentCapabilities()}
						var err error
						raw, err = r.MarshalCanonical()
						if err != nil {
							return nil, err
						}
						switch mode {
						case "empty":
							raw = nil
						case "partial":
							raw = raw[:len(raw)-2]
						case "extra":
							raw = append(raw, []byte("{}\n")...)
						}
					}
					if _, err := files[0].Write(raw); err != nil {
						return nil, err
					}
				}
				return child, nil
			}
			r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainExecutable: "/tmp/m", AgentExecutable: "/tmp/a", StartChild: starter})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.StartMain(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.StartAgent(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.ReadinessReceipt(context.Background()); err == nil {
				t.Fatal("invalid receipt accepted")
			}
			if terminated != 1 {
				t.Fatalf("agent terminate calls=%d, want 1", terminated)
			}
		})
	}
}

func TestSupervisorRuntimeHostRejectsReadinessFDOverrideWithoutStartingAgent(t *testing.T) {
	started := 0
	starter := func(context.Context, string, []string, []string, []*os.File) (*ChildProcess, error) {
		started++
		return newFakeChild(), nil
	}
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainExecutable: "/tmp/m", AgentExecutable: "/tmp/a", AgentArguments: []string{"--readiness-fd=9"}, StartChild: starter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartAgent(context.Background()); err == nil {
		t.Fatal("readiness fd override accepted")
	}
	if started != 1 {
		t.Fatalf("starter calls=%d, want only Main", started)
	}
}

func TestSupervisorRuntimeHostWaitChildrenUsesExactHandlesAndDeadline(t *testing.T) {
	mainExited := make(chan struct{})
	waitStarted := make(chan struct{}, 2)
	mainWaits, agentWaits := 0, 0
	starter := func(_ context.Context, path string, _ []string, _ []string, _ []*os.File) (*ChildProcess, error) {
		if path == "/tmp/m" {
			return &ChildProcess{waitFn: func(context.Context) error {
				mainWaits++
				waitStarted <- struct{}{}
				<-mainExited
				return errors.New("main exit")
			}}, nil
		}
		return &ChildProcess{waitFn: func(context.Context) error {
			agentWaits++
			waitStarted <- struct{}{}
			select {
			case <-mainExited:
				return nil
			case <-time.After(time.Second):
				return errors.New("agent exit")
			}
		}}, nil
	}
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainExecutable: "/tmp/m", AgentExecutable: "/tmp/a", StartChild: starter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- r.WaitChildren(context.Background()) }()
	<-waitStarted
	<-waitStarted
	close(mainExited)
	if err := <-waitResult; err == nil {
		t.Fatal("child exit not reported")
	}
	if mainWaits != 1 || agentWaits != 1 {
		t.Fatalf("wait calls main=%d agent=%d, want 1/1", mainWaits, agentWaits)
	}

	blocked := &ChildProcess{waitFn: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	r2 := &SupervisorRuntime{main: blocked}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r2.WaitChildren(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error=%v", err)
	}
}

func TestSupervisorRuntimeHostRejectsUnsafeReadinessInputs(t *testing.T) {
	dir := t.TempDir()
	regular, state, menu, core := filepath.Join(dir, "cmd"), filepath.Join(dir, "state"), filepath.Join(dir, "menu"), filepath.Join(dir, "core")
	for path, data := range map[string]string{regular: "not fifo", state: "operating\n", menu: "menu", core: "MENU\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainFIFO: regular, FPGAManagerState: state, MenuPath: menu, CoreNameFile: core})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CommandFIFOReady(); err == nil {
		t.Fatal("regular file accepted as FIFO")
	}
	unsafeFIFO := filepath.Join(dir, "unsafe-fifo")
	if err := mkfifoRuntimeTest(unsafeFIFO); err != nil {
		t.Fatal(err)
	}
	r.config.MainFIFO = unsafeFIFO
	if err := os.Chmod(unsafeFIFO, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.CommandFIFOReady(); err == nil {
		t.Fatal("FIFO with unsafe mode accepted")
	}
	if err := r.FPGAManagerReady(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("not operating\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.FPGAManagerReady(); err == nil {
		t.Fatal("non-operating state accepted")
	}
	if err := os.WriteFile(state, []byte("operating\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("NOT-MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.MenuReady(); err == nil {
		t.Fatal("mutated CORENAME accepted")
	}
}

func TestSupervisorRuntimeHostDetectsCORENAMEMutationBetweenReads(t *testing.T) {
	core := filepath.Join(t.TempDir(), "CORENAME")
	if err := mkfifoRuntimeTest(core); err != nil {
		t.Fatal(err)
	}
	go func() {
		for _, value := range []string{"MENU\n", "OTHER\n"} {
			f, err := os.OpenFile(core, os.O_WRONLY, 0)
			if err != nil {
				return
			}
			_, _ = f.WriteString(value)
			_ = f.Close()
		}
	}()
	r, err := NewSupervisorRuntime(SupervisorRuntimeConfig{CoreNameFile: core})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MenuReady(); err == nil {
		t.Fatal("CORENAME mutation accepted")
	}
}
