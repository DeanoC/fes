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
func TestArmDoesNotOpenOnPrepFailure(t *testing.T) {
	d := &fakeWatchdog{char: true}
	c, opened, _ := testConfig(t, d, &fakeHelper{})
	c.prepare = func() error { return errors.New("prep") }
	result, err := Arm(context.Background(), c)
	if err == nil || result.Armed != nil || *opened != 0 {
		t.Fatalf("result=%+v err=%v opened=%d", result, err, *opened)
	}
}
func TestArmBusy(t *testing.T) {
	c, _, _ := testConfig(t, &fakeWatchdog{}, &fakeHelper{})
	started := false
	c.start = func(*exec.Cmd) (helper, error) { started = true; return &fakeHelper{}, nil }
	c.open = func(string) (watchdog, error) { return nil, unix.EBUSY }
	result, err := Arm(context.Background(), c)
	if err != nil || result.Armed != nil || started || result.Reason != "watchdog busy: trial guard owns it" {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(c.Marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestArmShortTimeoutMagicCloses(t *testing.T) {
	d := &fakeWatchdog{char: true, actual: 30 * time.Second}
	c, _, _ := testConfig(t, d, &fakeHelper{})
	result, _ := Arm(context.Background(), c)
	if result.Armed != nil || !d.closed || string(d.writes) != "V" {
		t.Fatalf("%+v %+v", result, d)
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

// A failure after the keepalive must leave the watchdog stopped (magic close),
// never armed without a handle that a failed reboot could cancel.
func TestArmFailureAfterKeepaliveDisarms(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Config)
	}{
		{"marker", func(c *Config) { c.Marker = filepath.Join(t.TempDir(), "missing", "marker") }},
		{"fallback", func(c *Config) {
			c.start = func(*exec.Cmd) (helper, error) { return nil, errors.New("no shell") }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeWatchdog{char: true, actual: 180 * time.Second}
			c, _, _ := testConfig(t, d, &fakeHelper{})
			tc.setup(&c)
			result, err := Arm(context.Background(), c)
			if err == nil || result.Armed != nil {
				t.Fatalf("arm: %+v %v", result, err)
			}
			if !reflect.DeepEqual(d.writes, []byte{0, 'V'}) || !d.closed {
				t.Fatalf("device not disarmed: writes=%q closed=%v", d.writes, d.closed)
			}
			if _, statErr := os.Stat(c.Marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("marker left: %v", statErr)
			}
		})
	}
}
