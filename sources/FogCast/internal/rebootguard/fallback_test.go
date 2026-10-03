package rebootguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runFallback runs the real helper script against a fake /proc and a fake
// reboot binary, returning how long it took to request `reboot -f`.
func runFallback(t *testing.T, stall, deadline int, progressing bool) time.Duration {
	t.Helper()
	dir := t.TempDir()
	stats := filepath.Join(dir, "diskstats")
	write := func(n int) {
		line := fmt.Sprintf(" 179       0 mmcblk0 1 0 2 0 %d 0 %d 0 0 0 0\n   7       0 loop0 1 0 2 0 0 0 0 0 0 0 0\n", n, n)
		if err := os.WriteFile(stats, []byte(line), 0o644); err != nil {
			t.Error(err)
		}
	}
	write(0)
	marker := filepath.Join(dir, "rebooted")
	reboot := filepath.Join(dir, "reboot")
	if err := os.WriteFile(reboot, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", fallbackScript, "fes-reboot-fallback", fmt.Sprint(stall), fmt.Sprint(deadline), "1", dir, reboot, "2")
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for n := 1; ; n++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(marker)
			if err != nil || strings.TrimSpace(string(got)) != "-f" {
				t.Fatalf("reboot args %q %v", got, err)
			}
			return time.Since(start)
		case <-time.After(300 * time.Millisecond):
			if progressing {
				write(n)
			}
			if time.Since(start) > 20*time.Second {
				_ = cmd.Process.Kill()
				t.Fatal("fallback never rebooted")
			}
		}
	}
}

func TestFallbackForcesRebootAfterIOStall(t *testing.T) {
	if took := runFallback(t, 2, 10, false); took < 2*time.Second || took > 5*time.Second {
		t.Fatalf("stalled shutdown: reboot -f after %s, want ~2 s", took)
	}
}

func TestFallbackWaitsForProgressingIOUntilDeadline(t *testing.T) {
	if took := runFallback(t, 2, 5, true); took < 5*time.Second || took > 8*time.Second {
		t.Fatalf("progressing shutdown: reboot -f after %s, want the 5 s hard deadline", took)
	}
}

func TestFallbackIgnoresTerm(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", fallbackScript, "fes-reboot-fallback", "30", "30", "1", dir, "/bin/true", "2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	time.Sleep(300 * time.Millisecond)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Process.Signal(sigterm)
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(sigzero); err != nil {
		t.Fatalf("helper died on TERM/INT: %v", err)
	}
}
