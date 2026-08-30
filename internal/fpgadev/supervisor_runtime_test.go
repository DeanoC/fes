//go:build linux && arm && fpgadev

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var runtimeReadinessFD = flag.Int("readiness-fd", 3, "supervisor readiness receipt fd")

func TestSupervisorRuntimeHelper(t *testing.T) {
	if os.Getenv("FOGCAST_RUNTIME_HELPER") == "" {
		return
	}
	if os.Getenv("FOGCAST_RUNTIME_AGENT") != "" {
		if os.Getenv("FOGCAST_RUNTIME_AGENT_EXIT") == "before-receipt" {
			// This is an abrupt helper fault used by the supervisor crash
			// matrix. No deferred cleanup is allowed to make the receipt appear.
			time.Sleep(50 * time.Millisecond)
			os.Exit(31)
		}
		if os.Getenv("FOGCAST_RUNTIME_AGENT_NO_RECEIPT") != "" {
			time.Sleep(30 * time.Second)
			return
		}
		attestation, err := CurrentProcessAttestation()
		if err != nil {
			os.Exit(21)
		}
		profile := sha256.Sum256([]byte("profile"))
		receipt := ReadinessReceipt{
			Schema: 1, PID: attestation.PID, StartTime: attestation.StartTime,
			ExecutableDevice: attestation.Device, ExecutableInode: attestation.Inode,
			ExecutableSHA256: attestation.SHA256, ProfileSHA256: hex.EncodeToString(profile[:]),
			Capabilities: DevelopmentCapabilities(),
		}
		if os.Getenv("FOGCAST_RUNTIME_RECEIPT") == "wrong-child" {
			receipt.PID++
		}
		raw, err := receipt.MarshalCanonical()
		if err != nil {
			os.Exit(22)
		}
		switch os.Getenv("FOGCAST_RUNTIME_RECEIPT") {
		case "empty":
			raw = nil
		case "partial":
			raw = raw[:len(raw)-2]
		case "extra":
			raw = append(raw, []byte("{}\n")...)
		}
		fd := *runtimeReadinessFD
		pipe := os.NewFile(uintptr(fd), "receipt")
		if pipe == nil {
			os.Exit(23)
		}
		if _, err := pipe.Write(raw); err != nil {
			os.Exit(23)
		}
		if err := pipe.Close(); err != nil {
			os.Exit(24)
		}
		if os.Getenv("FOGCAST_RUNTIME_AGENT_EXIT") == "after-receipt" {
			// The receipt has crossed the pipe boundary, but the helper dies
			// before the supervisor can attest a live agent.
			time.Sleep(50 * time.Millisecond)
			os.Exit(32)
		}
		if holdMS, parseErr := strconv.Atoi(os.Getenv("FOGCAST_RUNTIME_AGENT_HOLD_MS")); parseErr == nil && holdMS > 0 {
			time.Sleep(time.Duration(holdMS) * time.Millisecond)
		} else if os.Getenv("FOGCAST_RUNTIME_AGENT_HOLD") != "" {
			time.Sleep(100 * time.Millisecond)
		}
		return
	}
	time.Sleep(30 * time.Second)
}

func TestSupervisorRuntimeRejectsReceiptPayloadMatrixAndReaps(t *testing.T) {
	for _, mode := range []string{"empty", "partial", "extra"} {
		t.Run(mode, func(t *testing.T) {
			profile := sha256.Sum256([]byte("profile"))
			runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
				MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
				AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
				AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_HOLD=1", "FOGCAST_RUNTIME_RECEIPT="+mode),
				ProfileSHA256:    hex.EncodeToString(profile[:]),
			})
			if err != nil {
				t.Fatal(err)
			}
			main, err := runtime.StartMain(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = main.TerminateAndReap(context.Background()) }()
			agent, err := runtime.StartAgent(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			receiptErr := error(nil)
			if _, err := runtime.ReadinessReceipt(ctx); err == nil {
				t.Fatalf("%s readiness payload was accepted", mode)
			} else {
				receiptErr = err
			}
			waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
			defer waitCancel()
			if err := agent.Wait(waitCtx); err != nil && !strings.Contains(err.Error(), "signal") {
				t.Fatalf("%s failed child was not reaped: wait=%v receipt=%v pid=%d", mode, err, receiptErr, agent.PID())
			}
		})
	}
}

func TestSupervisorRuntimeReceiptTimeoutTerminatesAndReapsExactAgent(t *testing.T) {
	profile := sha256.Sum256([]byte("profile"))
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_NO_RECEIPT=1"),
		ProfileSHA256:    hex.EncodeToString(profile[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	main, err := runtime.StartMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = main.TerminateAndReap(context.Background()) }()
	agent, err := runtime.StartAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := runtime.ReadinessReceipt(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout receipt error=%v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("failed receipt cleanup exceeded bound: %v", elapsed)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := agent.Wait(waitCtx); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("timed-out child was not reaped: %v", err)
	}
}

func TestSupervisorRuntimeRejectsAnyAgentReadinessFDOverride(t *testing.T) {
	for _, arg := range []string{"--readiness-fd", "--readiness-fd=9"} {
		t.Run(arg, func(t *testing.T) {
			runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
				MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
				AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper", arg},
				AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_HOLD=1"),
			})
			if err != nil {
				t.Fatal(err)
			}
			main, err := runtime.StartMain(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = main.TerminateAndReap(context.Background()) }()
			if _, err := runtime.StartAgent(context.Background()); err == nil {
				t.Fatal("agent readiness fd override was accepted")
			}
		})
	}
}

func TestSupervisorRuntimeWaitChildrenReportsExactMainExit(t *testing.T) {
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"0.1"},
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_NO_RECEIPT=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	main, err := runtime.StartMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = main.TerminateAndReap(context.Background()) }()
	if _, err := runtime.StartAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = runtime.WaitChildren(ctx)
	if err == nil || !errors.Is(err, ErrSupervisorChildExited) || !strings.Contains(err.Error(), "Main") {
		t.Fatalf("WaitChildren error=%v, want exact Main exit", err)
	}
	if err := runtime.TerminateChildren(context.Background()); err != nil {
		t.Fatalf("agent cleanup failed: %v", err)
	}
}

func TestSupervisorRuntimeTerminateChildrenReapsExactPair(t *testing.T) {
	profile := sha256.Sum256([]byte("profile"))
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_NO_RECEIPT=1"),
		ProfileSHA256:    hex.EncodeToString(profile[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	main, err := runtime.StartMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := runtime.StartAgent(context.Background())
	if err != nil {
		_ = main.TerminateAndReap(context.Background())
		t.Fatal(err)
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.TerminateChildren(cleanupCtx); err != nil {
		t.Fatal(err)
	}
	if err := main.Wait(cleanupCtx); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("Main was not reaped: %v", err)
	}
	if err := agent.Wait(cleanupCtx); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("agent was not reaped: %v", err)
	}
}

func TestSupervisorRuntimeWaitChildrenHonorsBoundedContext(t *testing.T) {
	profile := sha256.Sum256([]byte("profile"))
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_NO_RECEIPT=1"),
		ProfileSHA256:    hex.EncodeToString(profile[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartMain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.WaitChildren(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitChildren error=%v, want deadline", err)
	}
	if err := runtime.TerminateChildren(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRuntimeRejectsReceiptFromWrongChildAndReapsExactAgent(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := sha256.Sum256([]byte("profile"))
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_HOLD=1", "FOGCAST_RUNTIME_RECEIPT=wrong-child"),
		ProfileSHA256:    hex.EncodeToString(profile[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	main, err := runtime.StartMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = main.TerminateAndReap(context.Background()) }()
	agent, err := runtime.StartAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ReadinessReceipt(context.Background()); err == nil {
		t.Fatal("receipt from a different child was accepted")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := agent.Wait(waitCtx); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("failed receipt child was not reaped: %v", err)
	}
}

func TestSupervisorRuntimeStartsMainAndAgentWithPipe(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifo); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "fpga-state")
	if err := os.WriteFile(state, []byte("operating\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	menu := filepath.Join(root, "menu.rbf")
	if err := os.WriteFile(menu, []byte("menu"), 0o600); err != nil {
		t.Fatal(err)
	}
	coreName := filepath.Join(root, "CORENAME")
	if err := os.WriteFile(coreName, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := sha256.Sum256([]byte("profile"))
	var main *ChildProcess
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable: "/bin/sleep", MainArguments: []string{"30"},
		MainProcessScanner: retainedChildProcessScanner{child: func() *ChildProcess {
			return main
		}},
		MainFIFO: fifo, FPGAManagerState: state, MenuPath: menu, CoreNameFile: coreName,
		AgentExecutable: os.Args[0], AgentArguments: []string{"-test.run=TestSupervisorRuntimeHelper"},
		AgentEnvironment: append(os.Environ(), "FOGCAST_RUNTIME_HELPER=1", "FOGCAST_RUNTIME_AGENT=1", "FOGCAST_RUNTIME_AGENT_HOLD=1"),
		ProfileSHA256:    hex.EncodeToString(profile[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	main, err = runtime.StartMain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if main == nil || main.PID() <= 0 {
		t.Fatalf("main handle=%#v", main)
	}
	defer func() {
		_ = main.TerminateAndReap(context.Background())
	}()
	if err := runtime.MainExecutableReady(); err != nil {
		t.Logf("MainExecutableReady after PID %d: %v (%+v)", main.PID(), err, err)
		var scanErr processScanError
		if errors.As(err, &scanErr) {
			t.Logf("scanner operation=%q underlying=%#v", scanErr.operation, scanErr.err)
		}
	}
	readyCtx, readyCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readyCancel()
	if err := runtime.WaitMainReady(readyCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartMain(ctx); err == nil {
		_ = main.TerminateAndReap(context.Background())
		t.Fatal("second Main start unexpectedly succeeded")
	}
	agent, err := runtime.StartAgent(ctx)
	if err != nil {
		_ = main.TerminateAndReap(context.Background())
		t.Fatal(err)
	}
	if agent == nil || agent.PID() <= 0 {
		_ = main.TerminateAndReap(context.Background())
		_ = agent.TerminateAndReap(context.Background())
		t.Fatalf("agent handle=%#v", agent)
	}
	receipt, err := runtime.ReadinessReceipt(ctx)
	if err != nil {
		_ = main.TerminateAndReap(context.Background())
		_ = agent.TerminateAndReap(context.Background())
		t.Fatal(err)
	}
	if receipt.PID != uint64(agent.PID()) || receipt.ProfileSHA256 != hex.EncodeToString(profile[:]) {
		_ = main.TerminateAndReap(context.Background())
		_ = agent.TerminateAndReap(context.Background())
		t.Fatalf("receipt=%#v agent pid=%d", receipt, agent.PID())
	}
	if err := agent.Wait(context.Background()); err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("agent wait=%v", err)
	}
	if err := main.TerminateAndReap(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRuntime4cRetainsExactMainAttestation(t *testing.T) {
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable:  "/bin/sleep",
		MainArguments:   []string{"30"},
		AgentExecutable: os.Args[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	main, err := runtime.StartMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = main.TerminateAndReap(context.Background()) }()
	want, err := main.Attestation()
	if err != nil {
		t.Fatal(err)
	}
	got, err := runtime.MainAttestation()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Main attestation=%#v, want retained handle identity %#v", got, want)
	}
}

func TestSupervisorRuntimeCommandFIFORejectsWorldWritable(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fifo, 0o666); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{MainFIFO: fifo})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.CommandFIFOReady(); err == nil {
		t.Fatal("world-writable Main FIFO was accepted")
	}
}

// retainedChildProcessScanner keeps this real-child boundary fixture
// independent of host policy that hides unrelated /proc entries (for
// example, an unprivileged test cannot read PID 1's executable). It still
// obtains the identity through the production retained-child attestation and
// returns it through the same ProcessScanner contract used by Observer.
type retainedChildProcessScanner struct {
	child func() *ChildProcess
}

func (s retainedChildProcessScanner) Scan(ctx context.Context, expected ExecutableIdentity) ([]ProcessRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	child := s.child()
	if child == nil {
		return nil, errors.New("retained child is unavailable")
	}
	attestation, err := child.Attestation()
	if err != nil {
		return nil, err
	}
	identity := ProcessIdentity{
		PID: int(attestation.PID), StartTime: attestation.StartTime,
		Device: attestation.Device, Inode: attestation.Inode, SHA256: attestation.SHA256,
	}
	if !identity.executable().equal(expected) {
		return nil, errors.New("retained child executable identity changed")
	}
	return []ProcessRecord{{Identity: identity}}, nil
}

func TestReadinessReceiptPipeTimesOutWithoutDetachedReader(t *testing.T) {
	reader, writer, err := NewReadinessPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ReadReadinessReceiptFromPipe(ctx, reader); err == nil {
		t.Fatal("receipt read unexpectedly succeeded without EOF")
	}
}
