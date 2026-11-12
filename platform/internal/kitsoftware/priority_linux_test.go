package kitsoftware

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRoleSpawnPolicyLowersOnlyHostAndAgent(t *testing.T) {
	for _, role := range []string{roleHost, roleAgent} {
		policy := roleSpawnPolicy(role)
		if !policy.background || strings.Join(policy.env, " ") != "GOMAXPROCS=1" {
			t.Fatalf("%s policy = %+v", role, policy)
		}
	}
	for _, role := range []string{roleLauncher, roleRuntime, ""} {
		if policy := roleSpawnPolicy(role); policy.background || len(policy.env) != 0 {
			t.Fatalf("%q policy = %+v", role, policy)
		}
	}
}

// TestPriorityProcessFixture is the child of the spawn tests. It holds
// several OS threads open, reports its GOMAXPROCS and waits to be killed.
func TestPriorityProcessFixture(t *testing.T) {
	ready := os.Getenv("FES_TEST_PRIORITY_READY")
	if ready == "" {
		return
	}
	for range 4 {
		go func() {
			runtime.LockOSThread()
			for {
				time.Sleep(time.Hour)
			}
		}()
	}
	for threadCount(t, os.Getpid()) < 5 {
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(ready, []byte(strconv.Itoa(runtime.GOMAXPROCS(0))), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestSpawnRunsEveryHostAndAgentThreadAtBackgroundNice(t *testing.T) {
	base := threadNices(t, os.Getpid())[0]
	if base >= backgroundNice {
		t.Skipf("test process already runs at nice %d", base)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	startedBackground := false
	for _, tc := range []struct {
		role       string
		nice       int
		gomaxprocs string
	}{
		{roleHost, backgroundNice, "1"},
		{roleAgent, backgroundNice, "1"},
		{roleLauncher, base, ""},
		{roleRuntime, base, ""},
	} {
		t.Run(tc.role, func(t *testing.T) {
			t.Setenv("GOMAXPROCS", "")
			_ = os.Unsetenv("GOMAXPROCS")
			ready := filepath.Join(t.TempDir(), "ready")
			c := &ProcessController{RunDir: t.TempDir()}
			p, err := c.spawn(binary, []string{"-test.run=^TestPriorityProcessFixture$"}, []string{"FES_TEST_PRIORITY_READY=" + ready}, tc.role)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.cmd.Process.Kill(); <-p.done }()
			if tc.nice == backgroundNice {
				startedBackground = true
			}
			if strings.Contains(strings.Join(p.env, " "), "GOMAXPROCS") {
				t.Fatalf("restart env carries the role policy: %q", p.env)
			}
			reported := waitForFile(t, ready, p)
			environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", p.cmd.Process.Pid))
			if err != nil {
				t.Fatal(err)
			}
			hasLimit := strings.Contains("\x00"+string(environ), "\x00GOMAXPROCS=")
			if tc.gomaxprocs == "" && hasLimit {
				t.Fatal("GOMAXPROCS set for an unlimited role")
			}
			if tc.gomaxprocs != "" && (!hasLimit || reported != tc.gomaxprocs) {
				t.Fatalf("GOMAXPROCS = %s, want %s", reported, tc.gomaxprocs)
			}
			nices := threadNices(t, p.cmd.Process.Pid)
			if len(nices) < 5 {
				t.Fatalf("fixture has %d threads", len(nices))
			}
			for i, nice := range nices {
				if nice != tc.nice {
					t.Fatalf("thread %d of %d runs at nice %d, want %d (%v)", i, len(nices), nice, tc.nice, nices)
				}
			}
		})
	}
	// Only the forker's own thread in the supervisor is lowered. The forker
	// starts with the first background spawn, so a run filtered to the
	// launcher or runtime has nothing to count.
	if !startedBackground {
		return
	}
	lowered := 0
	for _, nice := range threadNices(t, os.Getpid()) {
		if nice == backgroundNice {
			lowered++
		} else if nice != base {
			t.Fatalf("supervisor thread at nice %d", nice)
		}
	}
	if lowered != 1 {
		t.Fatalf("%d supervisor threads at nice %d, want the forker alone", lowered, backgroundNice)
	}
}

func waitForFile(t *testing.T, path string, p *child) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) != 0 {
			return string(data)
		}
		select {
		case <-p.done:
			t.Fatalf("fixture exited: %v", p.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not report")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func threadCount(t *testing.T, pid int) int {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// threadNices reads the nice field of every thread of pid. The main thread
// is first.
func threadNices(t *testing.T, pid int) []int {
	t.Helper()
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		t.Fatal(err)
	}
	nices := make([]int, 0, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/stat", pid, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Fields after the parenthesised comm start at field 3 (state);
		// nice is field 19.
		_, rest, ok := strings.Cut(string(data[strings.LastIndexByte(string(data), ')'):]), ") ")
		fields := strings.Fields(rest)
		if !ok || len(fields) < 17 {
			t.Fatalf("thread stat %q", data)
		}
		nice, err := strconv.Atoi(fields[16])
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() == strconv.Itoa(pid) {
			nices = append([]int{nice}, nices...)
		} else {
			nices = append(nices, nice)
		}
	}
	return nices
}
