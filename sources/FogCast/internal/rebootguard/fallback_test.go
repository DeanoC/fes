package rebootguard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Only the fixture's sleep and sync commands are replaced. The production
// script still makes every stall, deadline, bounded-sync and reset decision.
const fallbackFixture = `
test_ticks=0
write_stats() {
 printf ' 179 0 mmcblk0 1 0 2 0 %s 0 %s 0 0 0 0\n' "$test_ticks" "$test_ticks" > "$FES_REBOOT_TEST_DIR/diskstats"
}
write_stats
sleep() {
 if [ -z "${sync_pid-}" ]; then
  test_ticks=$((test_ticks + 1))
  printf '%s\n' "$1" >> "$FES_REBOOT_TEST_DIR/polls"
  case "$FES_REBOOT_PROGRESS:$test_ticks" in
   always:*|second:2) write_stats ;;
  esac
 else
  printf '%s\n' "$1" >> "$FES_REBOOT_TEST_DIR/sync-polls"
 fi
}
sync() {
 printf started > "$FES_REBOOT_TEST_DIR/sync-started"
 if [ "$FES_REBOOT_SYNC" = blocked ]; then
  IFS= read -r token < "$FES_REBOOT_TEST_DIR/sync-gate"
 fi
 printf done > "$FES_REBOOT_TEST_DIR/sync-done"
}
`

type fallbackCase struct {
	stall, deadline, poll int
	progress, sysrq       string
	blockedSync           bool
}

type fallbackResult struct {
	polls, syncPolls []string
}

func runFallback(t *testing.T, c fallbackCase) fallbackResult {
	t.Helper()
	dir := t.TempDir()
	sysrq := filepath.Join(dir, "sysrq-trigger")
	switch c.sysrq {
	case "missing":
	case "directory":
		if err := os.Mkdir(sysrq, 0o700); err != nil {
			t.Fatal(err)
		}
	default:
		if err := os.WriteFile(sysrq, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gatePath := filepath.Join(dir, "sync-gate")
	if err := syscall.Mkfifo(gatePath, 0o600); err != nil {
		t.Fatal(err)
	}
	// RDWR opens without a writer/reader rendezvous. Cleanup can always release
	// our background sync, including when an assertion or the child fails.
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("release\n") }()
	reboot := filepath.Join(dir, "reboot")
	if err := os.WriteFile(reboot, []byte(`#!/bin/sh
printf '%s\n' "$*" > "$FES_REBOOT_TEST_DIR/rebooted"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fallbackFixture+fallbackScript, "fes-reboot-fallback", fmt.Sprint(c.stall), fmt.Sprint(c.deadline), fmt.Sprint(c.poll), dir, reboot, "2", sysrq)
	syncMode := "complete"
	if c.blockedSync {
		syncMode = "blocked"
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.Env = append(os.Environ(), "FES_REBOOT_TEST_DIR="+dir, "FES_REBOOT_PROGRESS="+c.progress, "FES_REBOOT_SYNC="+syncMode)
	// Files deliberately avoid CombinedOutput's wait for a background child's
	// inherited output pipes after the fallback process itself has exited.
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		data, _ := os.ReadFile(output.Name())
		t.Fatalf("fallback: %v: %s", err, data)
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := strings.TrimSpace(read("rebooted")); got != "-nf" {
		t.Fatalf("reboot args %q", got)
	}
	if c.sysrq == "" && read("sysrq-trigger") != "b" {
		t.Fatal("sysrq reset was not attempted")
	}
	waitFile := func(name string) {
		t.Helper()
		for read(name) == "" {
			select {
			case <-ctx.Done():
				t.Fatalf("fixture never wrote %s: %v", name, ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitFile("sync-started")
	if c.blockedSync && read("sync-done") != "" {
		t.Fatal("blocked sync completed before fallback exited")
	}
	result := fallbackResult{polls: strings.Fields(read("polls")), syncPolls: strings.Fields(read("sync-polls"))}
	if len(result.syncPolls) > 2 {
		t.Fatalf("sync exceeded its two polls: %v", result.syncPolls)
	}
	_, _ = gate.WriteString("release\n")
	waitFile("sync-done")
	return result
}

func requirePolls(t *testing.T, r fallbackResult, count, poll int) {
	t.Helper()
	if len(r.polls) != count {
		t.Fatalf("polls %v, want %d steps of %ds", r.polls, count, poll)
	}
	for _, got := range r.polls {
		if got != fmt.Sprint(poll) {
			t.Fatalf("poll duration %q, want %d", got, poll)
		}
	}
}

func TestFallbackWritesSysrqAndForcesRebootAfterIOStall(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 2, deadline: 10, poll: 1}), 2, 1)
}
func TestFallbackUsesRebootWhenSysrqPathMissing(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 1, deadline: 3, poll: 1, sysrq: "missing"}), 1, 1)
}
func TestFallbackUsesRebootWhenSysrqWriteFails(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 1, deadline: 3, poll: 1, sysrq: "directory"}), 1, 1)
}
func TestFallbackWaitsForProgressingIOUntilDeadline(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 2, deadline: 5, poll: 1, progress: "always"}), 5, 1)
}
func TestFallbackRestartsStallWindowAfterProgress(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 2, deadline: 10, poll: 1, progress: "second"}), 4, 1)
}
func TestFallbackCountsConfiguredPollInterval(t *testing.T) {
	requirePolls(t, runFallback(t, fallbackCase{stall: 10, deadline: 30, poll: 5}), 2, 5)
}
func TestFallbackDoesNotWaitForBlockedSync(t *testing.T) {
	r := runFallback(t, fallbackCase{stall: 1, deadline: 3, poll: 1, blockedSync: true})
	requirePolls(t, r, 1, 1)
	if len(r.syncPolls) != 2 {
		t.Fatalf("blocked sync polls %v, want two", r.syncPolls)
	}
}

func TestFallbackIgnoresTerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	// Reaching sleep proves the production signal traps are installed. The
	// second rendezvous proves forward progress after TERM/INT, without sleeps.
	const rendezvous = `
sync() { :; }
sleep() {
 printf . >> "$FES_REBOOT_TEST_DIR/ready"
 IFS= read -r token < "$FES_REBOOT_TEST_DIR/gate"
}
`
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", rendezvous+fallbackScript, "fes-reboot-fallback", "30", "30", "1", dir, "/bin/true", "2", filepath.Join(dir, "missing-sysrq"))
	cmd.Env = append(os.Environ(), "FES_REBOOT_TEST_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	wait := func(n int) {
		t.Helper()
		for {
			data, err := os.ReadFile(filepath.Join(dir, "ready"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(data) >= n {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("helper did not reach rendezvous %d: %v", n, ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	wait(1)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(sigterm); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.WriteString("continue\n"); err != nil {
		t.Fatal(err)
	}
	wait(2)
	if err := cmd.Process.Signal(sigzero); err != nil {
		t.Fatalf("helper died on TERM/INT: %v", err)
	}
}
