package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeInitializationFixture(t *testing.T) {
	path := os.Getenv("FES_TEST_NATIVE_INIT_READY")
	if path == "" {
		return
	}
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(path, []byte("physical initialization finished"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestEarlyAgentFailureFixture(t *testing.T) {
	path := os.Getenv("FES_TEST_AGENT_STARTED")
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte("agent started"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(2)
}
func nativeChild(t *testing.T, c *ProcessController, path string, args, env []string) *child {
	t.Helper()
	if err := c.start(path, args, env, ""); err != nil {
		t.Fatal(err)
	}
	p := c.children[len(c.children)-1]
	t.Cleanup(func() { _ = p.cmd.Process.Kill(); <-p.done })
	return p
}
func TestEarlyAgentFailureCannotInterruptNativeInitialization(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "native-ready")
	agent := filepath.Join(dir, "agent-started")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &ProcessController{RunDir: dir}
	runtime := nativeChild(t, c, binary, []string{"-test.run=^TestNativeInitializationFixture$"}, []string{"FES_TEST_NATIVE_INIT_READY=" + ready})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	checks := 0
	probe := func(probeCtx context.Context, socket string, pid int, start, executable string) (nativeStatus, error) {
		checks++
		got, ok := probeCtx.Deadline()
		if !ok || !got.Equal(deadline) {
			t.Fatal("native wait reset startup deadline")
		}
		if pid != runtime.cmd.Process.Pid || start == "" || executable != binary {
			t.Fatal("runtime identity missing")
		}
		if _, err := os.Stat(agent); err == nil {
			t.Fatal("agent ran before native readiness")
		}
		if _, err := os.Stat(ready); err != nil {
			if checks%2 == 0 {
				return nativeStatus{}, syscall.ECONNREFUSED
			}
			return nativeStatus{}, os.ErrNotExist
		}
		return nativeStatus{Protocol: 2, OK: true, State: "idle", Execution: "none"}, nil
	}
	if err := c.startAgentAfterNativeInitialization(ctx, runtime, binary, []string{"-test.run=^TestEarlyAgentFailureFixture$"}, []string{"FES_TEST_AGENT_STARTED=" + agent}, "unused", probe); err != nil {
		t.Fatal(err)
	}
	failed := c.children[1]
	select {
	case <-failed.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if failed.cmd.ProcessState.ExitCode() != 2 || checks < 2 {
		t.Fatal("fixture did not fail after gated initialization")
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatal("agent failure preceded native completion", err)
	}
	select {
	case <-runtime.done:
		t.Fatal("agent failure terminated runtime during gate")
	default:
	}
	if _, err := os.Stat(agent); err != nil {
		t.Fatal(err)
	}
}
func TestNativeStartupFailureDoesNotLaunchAgent(t *testing.T) {
	for _, failure := range []string{"not-idle", "reboot-required", "failed-not-ok", "failed-wrong-code", "failed-wrong-phase", "failed-executing", "wrong-peer", "timeout", "runtime-exit", "cancelled-success"} {
		t.Run(failure, func(t *testing.T) {
			c := &ProcessController{RunDir: t.TempDir()}
			runtime := nativeChild(t, c, "/bin/sleep", []string{"60"}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			var probe nativeStatusProbe
			switch failure {
			case "wrong-peer":
				socket := filepath.Join(t.TempDir(), "native.sock")
				listener, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				go func() {
					connection, err := listener.Accept()
					if err == nil {
						defer connection.Close()
						<-ctx.Done()
					}
				}()
				probe = func(ctx context.Context, _ string, pid int, start, path string) (nativeStatus, error) {
					return readNativeStatusPeer(ctx, socket, pid, uint32(os.Getuid()), start, path)
				}
			default:
				probe = func(ctx context.Context, _ string, _ int, _, _ string) (nativeStatus, error) {
					switch failure {
					case "not-idle":
						return nativeStatus{Protocol: 2, OK: true, State: "running", Execution: "game"}, nil
					case "reboot-required":
						return nativeStatus{Protocol: 2, OK: true, State: "reboot_required", Execution: "none"}, nil
					case "failed-not-ok", "failed-wrong-code", "failed-wrong-phase", "failed-executing":
						status := nativeStatus{Protocol: 2, OK: true, State: "reboot_required", Execution: "none", Error: &nativeStatusError{Code: "idle_failed", Phase: "recovery"}}
						switch failure {
						case "failed-not-ok":
							status.OK = false
						case "failed-wrong-code":
							status.Error.Code = "io_failed"
						case "failed-wrong-phase":
							status.Error.Phase = "request"
						case "failed-executing":
							status.Execution = "game"
						}
						return status, nil
					case "runtime-exit":
						_ = runtime.cmd.Process.Kill()
						<-runtime.done
						return nativeStatus{}, os.ErrNotExist
					case "cancelled-success":
						cancel()
						return nativeStatus{Protocol: 2, OK: true, State: "idle", Execution: "none"}, nil
					default:
						return nativeStatus{}, syscall.ECONNREFUSED
					}
				}
			}
			started := time.Now()
			err := c.startAgentAfterNativeInitialization(ctx, runtime, "/bin/false", nil, nil, "unused", probe)
			if err == nil {
				t.Fatal("failed native initialization admitted agent")
			}
			if failure == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if failure == "runtime-exit" && !errors.Is(err, ErrProcessExited) {
				t.Fatal(err)
			}
			if len(c.children) != 1 || c.active != "" {
				t.Fatal("failed startup admitted another service")
			}
			if time.Since(started) > time.Second {
				t.Fatal("native wait reset or ignored caller deadline")
			}
		})
	}
}

// Only process launch is substituted here; health is the production controller's
// exact process/agent readiness check, and transitions use the real journal.
type diagnosticHealthControl struct {
	fakeControl
	process *ProcessController
}

func (c *diagnosticHealthControl) Start(ctx context.Context, id string, r Request) error {
	c.process.active = id
	return c.fakeControl.Start(ctx, id, r)
}
func (c *diagnosticHealthControl) Healthy(ctx context.Context, id string) error {
	return c.process.Healthy(ctx, id)
}
func TestCompletedNativeFailureStartsDiagnosticsButCannotAccept(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir()}
	runtime := nativeChild(t, c, "/bin/sleep", []string{"60"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	probe := func(context.Context, string, int, string, string) (nativeStatus, error) {
		return nativeStatus{Protocol: 2, OK: true, State: "reboot_required", Execution: "none", Error: &nativeStatusError{Code: "idle_failed", Phase: "recovery"}}, nil
	}
	if err := c.startAgentAfterNativeInitialization(ctx, runtime, "/bin/sleep", []string{"60"}, nil, "unused", probe); err != nil {
		t.Fatal(err)
	}
	agent := c.children[1]
	t.Cleanup(func() { _ = agent.cmd.Process.Kill(); <-agent.done })
	nativeChild(t, c, "/bin/sleep", []string{"60"}, nil)
	checks := 0
	c.Admission = &UnixAdmission{client: &http.Client{Transport: acknowledgmentTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/status" {
			t.Fatal("health mutated admission", r.URL.Path)
		}
		checks++
		raw, _ := json.Marshal(AdmissionStatus{BundleID: c.active, RawIdleReady: false})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})}}
	repo, engine, _, admission := setup(t)
	control := &diagnosticHealthControl{process: c}
	engine.Controller = control
	if err := Queue(repo, req()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err == nil {
		t.Fatal("physical failure accepted")
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if checks == 0 || state.Pending == nil || state.Pending.Phase != "blocked" || state.Selected != testFactoryID || state.Generation != 0 || state.Unreleased != nil || admission.releases != 0 || control.starts != 2 {
		t.Fatalf("diagnostic services admitted unhealthy selection: state=%+v checks=%d releases=%d starts=%d", state, checks, admission.releases, control.starts)
	}
	for _, p := range c.children {
		select {
		case <-p.done:
			t.Fatal("diagnostic child exited")
		default:
		}
	}
}
