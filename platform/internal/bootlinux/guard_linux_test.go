//go:build linux

package bootlinux

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeWatchdog struct {
	writes      []string
	closed      bool
	failPing    bool
	shortWrite  bool
	failTimeout bool
}

func (d *fakeWatchdog) SetTimeout(time.Duration) (time.Duration, error) {
	if d.failTimeout {
		return 0, errors.New("ioctl failed")
	}
	return time.Second, nil
}
func (d *fakeWatchdog) Write(p []byte) (int, error) {
	if d.failPing {
		return 0, errors.New("device failed")
	}
	if d.shortWrite {
		return 0, nil
	}
	d.writes = append(d.writes, string(p))
	return len(p), nil
}
func (d *fakeWatchdog) Close() error { d.closed = true; return nil }

func guardFixture() GuardConfig {
	return GuardConfig{Device: "/dev/watchdog", BootID: "a342c300-c2cb-47cb-92d1-21b92492dfe1", ImageSHA256: strings.Repeat("a", 64), TrialTimeout: 40 * time.Millisecond, HeartbeatInterval: time.Millisecond, DeviceTimeout: time.Second}
}

// A lost confirmation, canceled child, or broken pipe must never disarm recovery.
func TestGuardFailuresNeverMagicClose(t *testing.T) {
	for _, failure := range []string{"deadline", "confirmation-error", "cancel", "ack", "ping", "short-write", "ioctl"} {
		t.Run(failure, func(t *testing.T) {
			d := &fakeWatchdog{failPing: failure == "ping", shortWrite: failure == "short-write", failTimeout: failure == "ioctl"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var ready io.Writer = &bytes.Buffer{}
			if failure == "ack" {
				ready = failingWriter{}
			}
			confirmed := func(context.Context, string, string) (bool, error) {
				if failure == "cancel" {
					cancel()
				}
				if failure == "confirmation-error" {
					return false, errors.New("torn state")
				}
				return false, nil
			}
			err := runGuard(ctx, guardFixture(), ready, confirmed, d)
			if err == nil {
				t.Fatal("failed trial succeeded")
			}
			for _, p := range d.writes {
				if strings.Contains(p, "V") {
					t.Fatal("failed trial disarmed watchdog")
				}
			}
			if !d.closed {
				t.Fatal("failed trial leaked watchdog descriptor")
			}
		})
	}
}

type failingWriter struct{}

func TestGuardRejectsUnboundedTrialAndInvalidIdentityBeforePinging(t *testing.T) {
	for _, invalid := range []string{"boot", "image", "deadline", "heartbeat"} {
		t.Run(invalid, func(t *testing.T) {
			cfg := guardFixture()
			switch invalid {
			case "boot":
				cfg.BootID = ""
			case "image":
				cfg.ImageSHA256 = "wrong"
			case "deadline":
				cfg.TrialTimeout = time.Hour
			case "heartbeat":
				cfg.HeartbeatInterval = cfg.DeviceTimeout
			}
			d := &fakeWatchdog{}
			err := runGuard(context.Background(), cfg, io.Discard, func(context.Context, string, string) (bool, error) {
				t.Fatal("invalid guard consulted confirmation")
				return true, nil
			}, d)
			if err == nil || len(d.writes) != 0 {
				t.Fatalf("invalid guard accepted: %v %+v", err, d)
			}
		})
	}
}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestGuardArmsBeforeAcknowledgementAndDisarmsOnlyExactConfirmation(t *testing.T) {
	d := &fakeWatchdog{}
	cfg := guardFixture()
	var ready bytes.Buffer
	calls := 0
	err := runGuard(context.Background(), cfg, &ready, func(_ context.Context, boot, image string) (bool, error) {
		calls++
		if boot != cfg.BootID || image != cfg.ImageSHA256 {
			t.Fatal("confirmation identity changed")
		}
		if ready.String() != "armed\n" || len(d.writes) == 0 {
			t.Fatal("confirmation checked before armed acknowledgement")
		}
		return calls == 3, nil
	}, d)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !d.closed || d.writes[len(d.writes)-1] != "V" {
		t.Fatalf("confirmation did not finish watchdog: %+v", d)
	}
}

func TestGuardDoesNotConfirmAfterDeadline(t *testing.T) {
	d := &fakeWatchdog{}
	cfg := guardFixture()
	cfg.TrialTimeout = time.Millisecond
	err := runGuard(context.Background(), cfg, io.Discard, func(context.Context, string, string) (bool, error) {
		time.Sleep(5 * time.Millisecond)
		return true, nil
	}, d)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late confirmation accepted: %v", err)
	}
	for _, p := range d.writes {
		if p == "V" {
			t.Fatal("late confirmation disarmed")
		}
	}
}

// Real child processes exercise pipe EOF, startup timeout, and descriptor passing.
func TestGuardChild(t *testing.T) {
	mode := os.Getenv("BOOTLINUX_TEST_CHILD")
	if mode == "" {
		return
	}
	f := os.NewFile(3, "ready")
	if mode == "ready" {
		_, _ = f.Write([]byte("armed\n"))
		_ = f.Close()
		os.Exit(0)
	}
	if mode == "wrong" {
		_, _ = f.Write([]byte("not-armed\n"))
		_ = f.Close()
		os.Exit(0)
	}
	if mode == "exit" {
		os.Exit(1)
	}
	time.Sleep(time.Hour)
}

func TestStartGuardRejectsExitWrongAcknowledgementAndTimeout(t *testing.T) {
	for _, mode := range []string{"ready", "wrong", "exit", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("BOOTLINUX_TEST_CHILD", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			cmd, err := startGuard(ctx, os.Args[0], []string{"-test.run=^TestGuardChild$"}, nil)
			if mode == "ready" {
				if err != nil {
					t.Fatal(err)
				}
				_ = cmd.Wait()
			} else if err == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("unarmed child accepted")
			}
		})
	}
}
