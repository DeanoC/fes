package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/rebootguard"
)

// Every command here is an isolated script. Both backstop seams are replaced
// before a recovery call: tests never open a watchdog or start its fallback.
func recoveryBackstop(t *testing.T) (*int, <-chan *rebootguard.Armed) {
	t.Helper()
	oldArm, oldCancel := armReboot, cancelReboot
	t.Cleanup(func() { armReboot, cancelReboot = oldArm, oldCancel })
	calls := new(int)
	canceled := make(chan *rebootguard.Armed, 2)
	armReboot = func(context.Context, rebootguard.Config) (rebootguard.Result, error) {
		(*calls)++
		return rebootguard.Result{Armed: &rebootguard.Armed{}, Reason: "test backstop"}, nil
	}
	cancelReboot = func(handle *rebootguard.Armed) error { canceled <- handle; return nil }
	return calls, canceled
}

func recoveryCommand(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reboot")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitRecoveryMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("recovery command marker missing")
}

func TestRecoveryRebootRefusesBeforeArming(t *testing.T) {
	calls, _ := recoveryBackstop(t)
	for _, kind := range []string{"missing", "directory", "not executable", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reboot")
			ctx := context.Background()
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "not executable":
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				path = recoveryCommand(t, "exit 0")
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := startRecoveryReboot(ctx, path); err == nil || *calls != 0 {
				t.Fatalf("rejected request err=%v arm calls=%d", err, *calls)
			}
		})
	}
}

func TestRecoveryRebootCancelsBackstopOnStartFailure(t *testing.T) {
	calls, canceled := recoveryBackstop(t)
	path := recoveryCommand(t, "exit 0")
	// Executable validation succeeds, then the command disappears during arm.
	originalArm := armReboot
	armReboot = func(ctx context.Context, cfg rebootguard.Config) (rebootguard.Result, error) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		return originalArm(ctx, cfg)
	}
	if err := startRecoveryReboot(context.Background(), path); err == nil || *calls != 1 {
		t.Fatalf("start failure err=%v arm calls=%d", err, *calls)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("failed start retained its backstop")
	}
}

func TestRecoveryRebootCancelsBackstopAfterFailedCommandRetires(t *testing.T) {
	calls, canceled := recoveryBackstop(t)
	if err := startRecoveryReboot(context.Background(), recoveryCommand(t, "exit 9")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("failed command was not reaped/canceled")
	}
	if *calls != 1 {
		t.Fatalf("arm calls=%d", *calls)
	}
}

type recoveryLogSignal struct {
	slog.Handler
	observed chan slog.Record
}

func (h recoveryLogSignal) Handle(_ context.Context, record slog.Record) error {
	h.observed <- record
	return nil
}

func TestRecoveryRebootRetainsBackstopWhenShutdownSignalsCommand(t *testing.T) {
	_, canceled := recoveryBackstop(t)
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	records := make(chan slog.Record, 8)
	slog.SetDefault(slog.New(recoveryLogSignal{Handler: slog.NewTextHandler(io.Discard, nil), observed: records}))
	if err := startRecoveryReboot(context.Background(), recoveryCommand(t, "kill -TERM \"$$\"")); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case record := <-records:
			if record.Message != "development reboot command failed" {
				continue
			}
			// A preceding nonzero-exit case can finish logging after its
			// cancellation is observed. Wait for the actual signaled waiter.
			signaled := false
			record.Attrs(func(attr slog.Attr) bool {
				if attr.Key == "error" {
					var exit *exec.ExitError
					value, ok := attr.Value.Any().(error)
					signaled = ok && errors.As(value, &exit) && !exit.ProcessState.Exited()
				}
				return true
			})
			if !signaled {
				continue
			}
			select {
			case <-canceled:
				t.Fatal("shutdown signal canceled its backstop")
			default:
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("signaled command was not reaped")
		}
	}
}

func TestRecoveryRebootSurvivesCancellationAfterArming(t *testing.T) {
	calls, canceled := recoveryBackstop(t)
	marker := filepath.Join(t.TempDir(), "started")
	path := recoveryCommand(t, ": > '"+marker+"'")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalArm := armReboot
	armReboot = func(armCtx context.Context, cfg rebootguard.Config) (rebootguard.Result, error) {
		cancel()
		if armCtx.Err() != nil {
			t.Fatal("caller cancellation reached committed arm")
		}
		return originalArm(armCtx, cfg)
	}
	if err := startRecoveryReboot(ctx, path); err != nil {
		t.Fatal(err)
	}
	waitRecoveryMarker(t, marker)
	if *calls != 1 {
		t.Fatalf("arm calls=%d", *calls)
	}
	select {
	case <-canceled:
		t.Fatal("committed reboot was canceled")
	default:
	}
}

func TestRecoveryRebootReturnsWhileCommittedCommandIsRunning(t *testing.T) {
	_, canceled := recoveryBackstop(t)
	dir := t.TempDir()
	started, gate, finished := filepath.Join(dir, "started"), filepath.Join(dir, "gate"), filepath.Join(dir, "finished")
	path := recoveryCommand(t, ": > '"+started+"'\nwhile test ! -f '"+gate+"'; do sleep 0.01; done\n: > '"+finished+"'")
	unblock := func() {
		if err := os.WriteFile(gate, nil, 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(unblock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- startRecoveryReboot(ctx, path) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		unblock()
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Fatal("recovery starter did not retire")
		}
		t.Fatal("recovery starter waited for the running command")
	}
	waitRecoveryMarker(t, started)
	cancel()
	unblock()
	waitRecoveryMarker(t, finished)
	select {
	case <-canceled:
		t.Fatal("caller cancellation killed the committed command")
	default:
	}
}

func TestRecoveryRebootKeepsExistingAttemptWhenBackstopIncomplete(t *testing.T) {
	_, canceled := recoveryBackstop(t)
	armReboot = func(context.Context, rebootguard.Config) (rebootguard.Result, error) {
		return rebootguard.Result{Reason: "test unavailable"}, errors.New("test unavailable")
	}
	marker := filepath.Join(t.TempDir(), "started")
	if err := startRecoveryReboot(context.Background(), recoveryCommand(t, ": > '"+marker+"'")); err != nil {
		t.Fatal(err)
	}
	waitRecoveryMarker(t, marker)
	select {
	case <-canceled:
		t.Fatal("canceled an absent backstop")
	default:
	}
}
