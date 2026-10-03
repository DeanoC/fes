//go:build linux

package rebootguard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fakeWatchdog struct {
	writes          []byte
	closed          bool
	timeout, actual time.Duration
	char            bool
}

func (d *fakeWatchdog) Write(b []byte) (int, error) {
	d.writes = append(d.writes, b...)
	return len(b), nil
}
func (d *fakeWatchdog) Close() error { d.closed = true; return nil }
func (d *fakeWatchdog) SetTimeout(v time.Duration) (time.Duration, error) {
	d.timeout = v
	return d.actual, nil
}
func (d *fakeWatchdog) CharDevice() (bool, error) { return d.char, nil }

type fakeHelper struct{ killed int }

func (h *fakeHelper) KillWait() error { h.killed++; return nil }
func testConfig(t *testing.T, d *fakeWatchdog, h *fakeHelper) (Config, *int, *exec.Cmd) {
	t.Helper()
	opened := new(int)
	var command *exec.Cmd
	c := Config{Marker: filepath.Join(t.TempDir(), "marker"), prepare: func() error { return nil }, open: func(string) (watchdog, error) { *opened++; return d, nil }, start: func(cmd *exec.Cmd) (helper, error) { command = cmd; return h, nil }}
	c.openNull = func() (*os.File, error) { return os.CreateTemp(t.TempDir(), "stdio") }
	return c, opened, command
}
func TestArmAndCancel(t *testing.T) {
	d := &fakeWatchdog{char: true, actual: 180 * time.Second}
	h := &fakeHelper{}
	c, opened, _ := testConfig(t, d, h)
	var command *exec.Cmd
	c.start = func(cmd *exec.Cmd) (helper, error) { command = cmd; return h, nil }
	result, err := Arm(context.Background(), c)
	if err != nil || result.Armed == nil || result.ActualTimeout != 180*time.Second {
		t.Fatalf("arm: %+v %v", result, err)
	}
	if *opened != 1 || d.timeout != 180*time.Second || !reflect.DeepEqual(d.writes, []byte{0}) || d.closed {
		t.Fatalf("device: %+v", d)
	}
	if command.Path != "/bin/sh" || !reflect.DeepEqual(command.Args, []string{"/bin/sh", "-c", "trap '' TERM HUP INT; sleep 90; exec /sbin/reboot -f"}) || command.SysProcAttr == nil || !command.SysProcAttr.Setsid || command.Stdin == nil || command.Stdout == nil || command.Stderr == nil {
		t.Fatalf("fallback: %+v", command)
	}
	marker, err := os.ReadFile(c.Marker)
	if err != nil || !strings.Contains(string(marker), "pid=") || !strings.Contains(string(marker), "deadline=") {
		t.Fatalf("marker: %q %v", marker, err)
	}
	if err := result.Armed.Cancel(); err != nil {
		t.Fatal(err)
	}
	if err := result.Armed.Cancel(); err != nil {
		t.Fatal(err)
	}
	if h.killed != 1 || !d.closed || !reflect.DeepEqual(d.writes, []byte{0, 'V'}) {
		t.Fatalf("cancel: %+v %+v", h, d)
	}
	if _, err := os.Stat(c.Marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker remains: %v", err)
	}
}

// Whenever the watchdog cannot be armed, the reboot -f fallback and marker are
// still installed (Sol P1 round 2), any opened device is magic-closed, and
// Cancel removes what was installed.
func TestArmSkippedWatchdogKeepsFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setup     func(*Config, *fakeWatchdog, *int)
		wantOpen  int
		wantWrite string
		wantSlept time.Duration
	}{
		{"prep", func(c *Config, _ *fakeWatchdog, _ *int) { c.prepare = func() error { return errors.New("prep") } }, 0, "", 0},
		{"busy", func(c *Config, _ *fakeWatchdog, n *int) {
			c.open = func(string) (watchdog, error) { *n++; return nil, unix.EBUSY }
		}, 49, "", 12 * time.Second},
		{"absent", func(c *Config, _ *fakeWatchdog, n *int) {
			c.open = func(string) (watchdog, error) { *n++; return nil, unix.ENOENT }
		}, 1, "", 0},
		{"short", func(_ *Config, d *fakeWatchdog, _ *int) { d.actual = 30 * time.Second }, 1, "V", 0},
		{"notchar", func(_ *Config, d *fakeWatchdog, _ *int) { d.char = false }, 1, "V", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeWatchdog{char: true, actual: 180 * time.Second}
			h := &fakeHelper{}
			c, opened, _ := testConfig(t, d, h)
			started := 0
			c.start = func(*exec.Cmd) (helper, error) { started++; return h, nil }
			var slept time.Duration
			c.sleep = func(v time.Duration) { slept += v }
			tc.setup(&c, d, opened)
			result, _ := Arm(context.Background(), c)
			if result.Armed == nil || !result.Fallback || result.Watchdog || started != 1 || !strings.HasPrefix(result.Reason, "fallback only: ") {
				t.Fatalf("result=%+v started=%d", result, started)
			}
			if *opened != tc.wantOpen || string(d.writes) != tc.wantWrite || (tc.wantWrite == "V") != d.closed || slept != tc.wantSlept {
				t.Fatalf("opened=%d writes=%q closed=%v slept=%s", *opened, d.writes, d.closed, slept)
			}
			if _, err := os.Stat(c.Marker); err != nil {
				t.Fatalf("marker missing: %v", err)
			}
			if err := result.Armed.Cancel(); err != nil {
				t.Fatal(err)
			}
			if h.killed != 1 || string(d.writes) != tc.wantWrite {
				t.Fatalf("cancel: killed=%d writes=%q", h.killed, d.writes)
			}
			if _, err := os.Stat(c.Marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("marker left: %v", err)
			}
		})
	}
}

// Layers are independent: a fallback or marker failure still arms the watchdog.
func TestArmWatchdogSurvivesOtherLayerFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		setup        func(*Config)
		wantFallback bool
	}{
		{"marker", func(c *Config) { c.Marker = filepath.Join(t.TempDir(), "missing", "marker") }, true},
		{"fallback", func(c *Config) {
			c.start = func(*exec.Cmd) (helper, error) { return nil, errors.New("no shell") }
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeWatchdog{char: true, actual: 180 * time.Second}
			c, _, _ := testConfig(t, d, &fakeHelper{})
			tc.setup(&c)
			result, err := Arm(context.Background(), c)
			if err == nil || result.Armed == nil || !result.Watchdog || result.Fallback != tc.wantFallback {
				t.Fatalf("arm: %+v %v", result, err)
			}
			if !reflect.DeepEqual(d.writes, []byte{0}) || d.closed {
				t.Fatalf("watchdog not armed: writes=%q closed=%v", d.writes, d.closed)
			}
			if err := result.Armed.Cancel(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.writes, []byte{0, 'V'}) || !d.closed {
				t.Fatalf("cancel did not disarm: writes=%q closed=%v", d.writes, d.closed)
			}
		})
	}
}

func TestDisarmStale(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		trial, marker, busy, open bool
		want                      string
	}{
		{"trial", true, false, false, false, ""}, {"marker", false, true, false, false, ""}, {"busy", false, false, true, true, ""}, {"stable", false, false, false, true, "V"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeWatchdog{char: true}
			c, opened, _ := testConfig(t, d, &fakeHelper{})
			if tc.marker {
				if err := os.WriteFile(c.Marker, []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.busy {
				c.open = func(string) (watchdog, error) { *opened++; return nil, unix.EBUSY }
			}
			DisarmStale(c, tc.trial)
			if (*opened != 0) != tc.open || string(d.writes) != tc.want || (tc.want != "V" && d.closed) || (tc.want == "V" && !d.closed) {
				t.Fatalf("opened=%d device=%+v", *opened, d)
			}
		})
	}
}

// Activation right after trial confirmation: the guard still holds the device
// until its next heartbeat writes 'V'. Arm must wait for it, then arm normally.
func TestArmWaitsForTrialGuardHandover(t *testing.T) {
	d := &fakeWatchdog{char: true, actual: 180 * time.Second}
	c, _, _ := testConfig(t, d, &fakeHelper{})
	attempts := 0
	c.open = func(string) (watchdog, error) {
		attempts++
		if attempts <= 3 {
			return nil, unix.EBUSY
		}
		return d, nil
	}
	var slept time.Duration
	c.sleep = func(v time.Duration) { slept += v }
	result, err := Arm(context.Background(), c)
	if err != nil || result.Armed == nil || attempts != 4 || slept != 750*time.Millisecond {
		t.Fatalf("result=%+v err=%v attempts=%d slept=%s", result, err, attempts, slept)
	}
	if !reflect.DeepEqual(d.writes, []byte{0}) || d.closed {
		t.Fatalf("device: %+v", d)
	}
	if err := result.Armed.Cancel(); err != nil {
		t.Fatal(err)
	}
}
