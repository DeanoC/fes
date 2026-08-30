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

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
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

func TestRunnerLiveDE10NanoMainStaysOnFourByteMENUAfterSuccessfulFIFODispatchFailsClosed(t *testing.T) {
	dir := t.TempDir()
	tmpCore := filepath.Join(dir, "tmp", "CORENAME")
	fatCore := filepath.Join(dir, "media", "fat", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(tmpCore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpCore, []byte("MENU"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, mainObserver := newCompatibilityMainReadinessFixture(t, tmpCore, fatCore)

	fixture := newTask7RunnerFixture(t)
	fixture.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	// Bound the production handoff verifier to keep this test fast while
	// preserving the live-shaped proof: unique Main, operating FPGA manager,
	// four-byte /tmp/CORENAME MENU, and absent /media/fat/CORENAME.
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof
	productionReadiness := NewCompatibilityMainReadiness(runtime, mainObserver)
	deps.readiness = boundedProgrammedReadiness{readiness: productionReadiness, handoff: productionReadiness, timeout: 50 * time.Millisecond}

	result, err := newFixtureRunner(deps).RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v, want handled fail-closed recovery", err)
	}
	if result.PrimaryCode != string(CodeMainHandoffTimeout) || result.Phase != ResultPhaseLoadAttempted {
		t.Fatalf("live-shaped handoff result = %#v, want main_handoff_timeout at load_attempted", result)
	}
	if fixture.fifo.calls != 1 || fixture.mapper.openCalls != 0 || len(result.PayloadHex) != 0 {
		t.Fatalf("load path = fifo:%d mailbox-opens:%d payload:%q, want successful single FIFO dispatch and no FPGA payload", fixture.fifo.calls, fixture.mapper.openCalls, result.PayloadHex)
	}
	if proof.priorCalls != 2 || proof.currentCalls != 0 {
		t.Fatalf("prior-boot proof calls = prior:%d current:%d, want admission plus pre-dispatch revalidation", proof.priorCalls, proof.currentCalls)
	}
	mainAfter, snapshotErr := mainObserver.Snapshot()
	if snapshotErr != nil || len(mainAfter) != 1 {
		t.Fatalf("Main population after dispatch = %#v, %v, want the one expected Main still present", mainAfter, snapshotErr)
	}

	fixture.store.mu.Lock()
	final := cloneTask7Record(fixture.store.record)
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.ActiveOwner != hardwareowner.OwnerCompatMain || final.FirstFailure != string(CodeMainHandoffTimeout) {
		t.Fatalf("fail-closed owner = %#v, want recovery_required compat_main with main_handoff_timeout", final)
	}
	if raw, readErr := os.ReadFile(tmpCore); readErr != nil || string(raw) != "MENU" {
		t.Fatalf("four-byte /tmp/CORENAME after dispatch = %q, %v", raw, readErr)
	}
	if _, statErr := os.Stat(fatCore); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("/media/fat/CORENAME after dispatch error = %v, want absent", statErr)
	}
	if raw, readErr := os.ReadFile(runtime.config.FPGAManagerState); readErr != nil || string(raw) != "operating\n" {
		t.Fatalf("FPGA manager after dispatch = %q, %v, want operating without a positive core-transition observation", raw, readErr)
	}
}

func TestCompatibilityMainHandoffWaitsForStableCORENAMELeaveMENU(t *testing.T) {
	dir := t.TempDir()
	tmpCore := filepath.Join(dir, "tmp", "CORENAME")
	fatCore := filepath.Join(dir, "media", "fat", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(tmpCore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpCore, []byte("MENU"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, tmpCore, fatCore)
	readiness := NewCompatibilityMainReadiness(runtime, observer)
	writeDone := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		writeDone <- os.WriteFile(tmpCore, []byte("Powerboat\n"), 0o600)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := readiness.WaitProgrammed(ctx); err != nil {
		t.Fatalf("WaitProgrammed() = %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityMainHandoffRejectsMENUAndMalformedCORENAME(t *testing.T) {
	tests := []string{"MENU", "MENU\n", "Power\tboat", "\x00core"}
	for _, value := range tests {
		t.Run(hex.EncodeToString([]byte(value)), func(t *testing.T) {
			dir := t.TempDir()
			core := filepath.Join(dir, "CORENAME")
			if err := os.WriteFile(core, []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, observer := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "missing"))
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			if err := NewCompatibilityMainReadiness(runtime, observer).WaitProgrammed(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("WaitProgrammed(%q) = %v, want deadline", value, err)
			}
		})
	}
}

func TestCompatibilityMainHandoffRequiresOperatingFPGAManager(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "CORENAME")
	if err := os.WriteFile(core, []byte("Powerboat"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "missing"))
	if err := os.WriteFile(runtime.config.FPGAManagerState, []byte("reset\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := NewCompatibilityMainReadiness(runtime, observer).WaitProgrammed(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitProgrammed() = %v, want deadline without operating FPGA manager", err)
	}
}

type boundedProgrammedReadiness struct {
	readiness readinessVerifier
	handoff   programmedHandoffVerifier
	timeout   time.Duration
}

func (r boundedProgrammedReadiness) Verify(ctx context.Context) error {
	return r.readiness.Verify(ctx)
}

func (r boundedProgrammedReadiness) WaitProgrammed(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.handoff.WaitProgrammed(bounded)
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

func TestCompatibilityMainReadinessRequiresUniqueMain(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "tmp", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(core), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, observer := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "media", "fat", "CORENAME"))
	first := ProcessIdentity{PID: 41, StartTime: 7, Device: observer.Expected.Device, Inode: observer.Expected.Inode, SHA256: observer.Expected.SHA256}
	second := first
	second.PID = 42
	second.StartTime = 8
	observer.Scanner = &fakeProcessScanner{scans: [][]ProcessRecord{{{Identity: first}, {Identity: second}}}}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := NewCompatibilityMainReadiness(runtime, observer).Verify(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify error=%v, want fail-closed deadline", err)
	}
}

func TestCompatibilityMainReadinessAcceptsUniqueLiveAppRestartMain(t *testing.T) {
	dir := t.TempDir()
	core := filepath.Join(dir, "tmp", "CORENAME")
	if err := os.MkdirAll(filepath.Dir(core), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("MENU"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, _ := newCompatibilityMainReadinessFixture(t, core, filepath.Join(dir, "media", "fat", "CORENAME"))
	procRoot := filepath.Join(dir, "proc")
	if err := os.MkdirAll(procRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "media", "fat", "MiSTer")
	menuPath := filepath.Join(dir, "media", "fat", "menu.rbf")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte("protected-main-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	presence, err := NewCompatibilityMainObserver(mainPath, menuPath, procRoot)
	if err != nil {
		t.Fatal(err)
	}
	presence.Expected.Device++
	presence.Expected.Inode++
	processDir := writeProcStatFixture(t, procRoot, 614, 257, "R")
	if err := os.Symlink(mainPath, filepath.Join(processDir, "exe")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "comm"), []byte("MiSTer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "cmdline"), []byte(mainPath+"\x00"+menuPath+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := NewCompatibilityMainReadiness(runtime, presence).Verify(context.Background()); err != nil {
		t.Fatalf("Verify unique /media/fat/MiSTer-shaped app_restart Main: %v", err)
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
	if err := os.Chmod(unsafeFIFO, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := r.CommandFIFOReady(); err == nil {
		t.Fatal("world-writable FIFO accepted")
	}
	symlinkFIFO := filepath.Join(dir, "symlink-fifo")
	if err := os.Symlink(unsafeFIFO, symlinkFIFO); err != nil {
		t.Fatal(err)
	}
	r.config.MainFIFO = symlinkFIFO
	if err := r.CommandFIFOReady(); err == nil {
		t.Fatal("FIFO symlink accepted")
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

func TestSupervisorRuntimeCommandFIFOAcceptsSupportedMainModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644} {
		t.Run(mode.String(), func(t *testing.T) {
			fifo := filepath.Join(t.TempDir(), "MiSTer_cmd")
			if err := mkfifoRuntimeTest(fifo); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(fifo, mode); err != nil {
				t.Fatal(err)
			}
			runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainFIFO: fifo})
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.CommandFIFOReady(); err != nil {
				t.Fatalf("CommandFIFOReady mode %04o: %v", mode, err)
			}
		})
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
