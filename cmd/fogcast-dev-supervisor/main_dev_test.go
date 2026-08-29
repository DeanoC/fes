//go:build linux && fpgadev

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

func TestSupervisorCommandRequiresOneInheritedFD(t *testing.T) {
	var out, errOut bytes.Buffer
	calls := 0
	runner := func(context.Context, fpgadev.Dependencies) error { calls++; return nil }
	if got := runSupervisor([]string{"--inherited-fd", "9"}, &out, &errOut, runner); got != supervisorExitOK || calls != 1 {
		t.Fatalf("exit=%d calls=%d out=%q err=%q", got, calls, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	calls = 0
	if got := runSupervisor([]string{"--inherited-fd", "-1"}, &out, &errOut, runner); got != supervisorExitUsage || calls != 0 {
		t.Fatalf("invalid fd exit=%d calls=%d", got, calls)
	}
}

func TestLinuxAMD64TaggedProductionSupervisorIsStub(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("host-boundary assertion applies to tagged linux/amd64")
	}
	deps := productionSupervisorDependencies(-1)
	if deps.Reset != nil || deps.StartMain != nil || deps.StartAgent != nil || deps.RebootRequester != nil {
		t.Fatalf("linux/amd64 production supervisor exposed target constructors: %#v", deps)
	}
}

func TestLinuxAMD64TaggedSourcesKeepTargetConstructorsBehindARMBoundary(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("host-boundary assertion applies to tagged linux/amd64")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate source root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	armSources := []string{
		filepath.Join(root, "cmd/fogcast-dev-supervisor/deps_arm.go"),
		filepath.Join(root, "internal/fpgadev/supervisor_reset_linux.go"),
		filepath.Join(root, "internal/fpgadev/child_linux.go"),
	}
	for _, path := range armSources {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "linux && arm && fpgadev") {
			t.Fatalf("%s is not ARM-gated", path)
		}
	}
	childSource, err := os.ReadFile(filepath.Join(root, "internal/fpgadev/child_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childSource), "Pdeathsig: syscall.SIGKILL") || !strings.Contains(string(childSource), "PidfdOpen") {
		t.Fatal("ARM child adapter lacks PDEATHSIG/pidfd contract")
	}
	depsSource, err := os.ReadFile(filepath.Join(root, "cmd/fogcast-dev-supervisor/deps_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ResetSupervisor", "/sbin/reboot", "StartChildProcess"} {
		if strings.Contains(string(depsSource), forbidden) {
			t.Fatalf("linux dependency composition directly references target constructor %q", forbidden)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "internal/fpgadev/child_stub.go"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "!linux || !arm || !fpgadev") {
			t.Fatalf("%s is not a complete non-ARM stub", path)
		}
		for _, forbidden := range []string{"/dev/mem", "ResetSupervisor", "/sbin/reboot", "exec.Command"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("stub %s contains target constructor %q", path, forbidden)
			}
		}
	}
	runtimeSource, err := os.ReadFile(filepath.Join(root, "internal/fpgadev/supervisor_runtime_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeSource), "linux && fpgadev") {
		t.Fatal("supervisor runtime is not available on tagged Linux hosts")
	}
	for _, forbidden := range []string{"/dev/mem", "ResetSupervisor", "/sbin/reboot"} {
		if strings.Contains(string(runtimeSource), forbidden) {
			t.Fatalf("platform-neutral runtime contains target-only constructor %q", forbidden)
		}
	}
}
