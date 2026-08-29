//go:build linux && fpgadev

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"golang.org/x/sys/unix"
)

func TestProductionSupervisorDependenciesStrictlyBindProtectedProfile(t *testing.T) {
	if runtime.GOARCH != "arm" {
		t.Skip("real child-start dependency composition is ARM-only")
	}
	root := t.TempDir()
	configPath := filepath.Join(root, "agent.toml")
	fifoPath := filepath.Join(root, "main.cmd")
	menuPath := filepath.Join(root, "menu.rbf")
	managerPath := filepath.Join(root, "fpga-state")
	mainPath := filepath.Join(root, "Main")
	agentPath := filepath.Join(root, "agent-helper.sh")
	argsLog := filepath.Join(root, "agent-args")
	var mainChild *fpgadev.ChildProcess
	mainBytes, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, mainBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(menuPath, []byte("menu"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CORENAME"), []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managerPath, []byte("operating\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSupervisorAgentHelper(agentPath, argsLog); err != nil {
		t.Fatal(err)
	}
	if err := writeSupervisorConfig(configPath, fifoPath, menuPath, root); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}

	deps, err := newSupervisorDependencies(supervisorDependencyOptions{
		ConfigPath:      configPath,
		MainExecutable:  mainPath,
		MainArguments:   []string{"30"},
		ManagerState:    managerPath,
		AgentExecutable: agentPath,
		MainProcessScanner: supervisorFixtureProcessScanner{
			child: func() *fpgadev.ChildProcess { return mainChild },
		},
	}, -1)
	if err != nil {
		t.Fatalf("strict production composition: %v", err)
	}
	identity, ok := deps.SupervisorIdentity.(fpgadev.ProcessAttestation)
	if deps.ProfileSHA256 == "" || !ok || identity.PID == 0 || identity.StartTime == 0 {
		t.Fatalf("composition omitted protected profile/supervisor identity: %#v", deps)
	}
	if !deps.AllowAbsentOwner {
		t.Fatal("production supervisor composition does not permit successor-boot absent-owner bootstrap")
	}
	startMain, ok := deps.StartMain.(func(context.Context) (*fpgadev.ChildProcess, error))
	if !ok || startMain == nil {
		t.Fatalf("StartMain is not a retained production handle: %T", deps.StartMain)
	}
	mainReady, ok := deps.MainReadiness.(func(context.Context) error)
	if !ok || mainReady == nil {
		t.Fatalf("MainReadiness is not concrete: %T", deps.MainReadiness)
	}
	startAgent, ok := deps.StartAgent.(func(context.Context) (*fpgadev.ChildProcess, error))
	if !ok || startAgent == nil {
		t.Fatalf("StartAgent is not a retained production handle: %T", deps.StartAgent)
	}
	children, ok := deps.Children.(interface {
		TerminateChildren(context.Context) error
	})
	if !ok {
		t.Fatalf("Children is not the retained runtime: %T", deps.Children)
	}
	main, err := startMain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if main == nil {
		t.Fatal("StartMain returned no retained handle")
	}
	mainChild = main
	defer func() { _ = children.TerminateChildren(context.Background()) }()
	readyContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := mainReady(readyContext); err != nil {
		cancel()
		t.Fatalf("Main readiness did not use protected FIFO/menu/state paths: %v", err)
	}
	cancel()
	if _, err := startAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	var args string
	deadline := time.Now().Add(2 * time.Second)
	wantArgs := "--config\n" + configPath + "\n--readiness-fd\n3\n"
	for time.Now().Before(deadline) {
		raw, readErr := os.ReadFile(argsLog)
		if readErr == nil {
			args = string(raw)
			if args == wantArgs {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if args != wantArgs {
		t.Fatalf("agent argv=%q, want=%q", args, wantArgs)
	}
}

func TestProductionSupervisorDependenciesRejectInvalidProtectedProfile(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "agent.toml")
	if err := os.WriteFile(configPath, []byte("not valid profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newSupervisorDependencies(supervisorDependencyOptions{ConfigPath: configPath, MainExecutable: "/bin/true"}, -1); err == nil {
		t.Fatal("invalid protected profile was accepted")
	}
}

func writeSupervisorConfig(path, fifo, menu, root string) error {
	content := fmt.Sprintf(`listen_address = "127.0.0.1:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = %q
core_name_file = %q
menu_rbf = %q
mgl_directory = %q
build_profile = "development"
development_profile = true
hardware_owner_path = %q
hardware_owner_lock = %q
designation_path = %q
target_identity_path = %q
fpgadev_boot_dispatcher = %q
fpgadev_start_sources = [%q]
`, fifo, filepath.Join(root, "CORENAME"), menu, root,
		filepath.Join(root, "owner.json"), filepath.Join(root, "owner.lock"),
		filepath.Join(root, "designation"), filepath.Join(root, "identity"),
		filepath.Join(root, "dispatcher.sh"), filepath.Join(root, "dispatcher.sh"))
	return os.WriteFile(path, []byte(content), 0o600)
}

func writeSupervisorAgentHelper(path, logPath string) error {
	content := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" > " + shellQuoteForSupervisorTest(logPath) + "\nexec /bin/sleep 30\n"
	return os.WriteFile(path, []byte(content), 0o755)
}

func shellQuoteForSupervisorTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

type supervisorFixtureProcessScanner struct {
	child func() *fpgadev.ChildProcess
}

func (s supervisorFixtureProcessScanner) Scan(ctx context.Context, expected fpgadev.ExecutableIdentity) ([]fpgadev.ProcessRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	child := s.child()
	if child == nil {
		return nil, fmt.Errorf("fixture Main child is unavailable")
	}
	identity, err := child.Attestation()
	if err != nil {
		return nil, err
	}
	if identity.Device != expected.Device || identity.Inode != expected.Inode || identity.SHA256 != expected.SHA256 {
		return nil, fmt.Errorf("fixture Main identity differs from expected executable")
	}
	return []fpgadev.ProcessRecord{{Identity: fpgadev.ProcessIdentity{
		PID: int(identity.PID), StartTime: identity.StartTime,
		Device: identity.Device, Inode: identity.Inode, SHA256: identity.SHA256,
	}}}, nil
}
