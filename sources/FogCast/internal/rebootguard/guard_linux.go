//go:build linux

package rebootguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Config controls the agent's reboot backstop. Zero values use production defaults.
type Config struct {
	Device          string
	WatchdogTimeout time.Duration
	MinTimeout      time.Duration
	FallbackDelay   time.Duration
	Marker          string
	// BusyWait bounds how long Arm waits for the fes-boot trial guard to
	// release /dev/watchdog. Confirm reopens update admission immediately, but
	// the guard only notices durable confirmation on its next 5 s heartbeat.
	BusyWait time.Duration
	BusyPoll time.Duration
	sleep    func(time.Duration)
	prepare  func() error
	open     func(string) (watchdog, error)
	start    func(*exec.Cmd) (helper, error)
	openNull func() (*os.File, error)
}

type watchdog interface {
	io.WriteCloser
	SetTimeout(time.Duration) (time.Duration, error)
	CharDevice() (bool, error)
}
type helper interface{ KillWait() error }

type deviceFile struct{ *os.File }

func (d *deviceFile) SetTimeout(timeout time.Duration) (time.Duration, error) {
	seconds := int((timeout + time.Second - 1) / time.Second)
	if err := unix.IoctlSetPointerInt(int(d.Fd()), unix.WDIOC_SETTIMEOUT, seconds); err != nil {
		return 0, err
	}
	actual, err := unix.IoctlGetInt(int(d.Fd()), unix.WDIOC_GETTIMEOUT)
	return time.Duration(actual) * time.Second, err
}
func (d *deviceFile) CharDevice() (bool, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(d.Fd()), &st); err != nil {
		return false, err
	}
	return st.Mode&unix.S_IFMT == unix.S_IFCHR, nil
}
func openDevice(path string) (watchdog, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return &deviceFile{os.NewFile(uintptr(fd), path)}, nil
}

type process struct{ *os.Process }

func (p process) KillWait() error {
	err := p.Kill()
	_, waitErr := p.Wait()
	return errors.Join(err, waitErr)
}
func startHelper(cmd *exec.Cmd) (helper, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return process{cmd.Process}, nil
}
func (c Config) defaults() Config {
	if c.Device == "" {
		c.Device = "/dev/watchdog"
	}
	if c.WatchdogTimeout == 0 {
		c.WatchdogTimeout = 180 * time.Second
	}
	if c.MinTimeout == 0 {
		c.MinTimeout = 60 * time.Second
	}
	if c.FallbackDelay == 0 {
		c.FallbackDelay = 90 * time.Second
	}
	if c.Marker == "" {
		c.Marker = "/run/fes-reboot-backstop"
	}
	if c.BusyWait == 0 {
		c.BusyWait = 12 * time.Second
	}
	if c.BusyPoll == 0 {
		c.BusyPoll = 250 * time.Millisecond
	}
	if c.sleep == nil {
		c.sleep = time.Sleep
	}
	if c.prepare == nil {
		c.prepare = PrepareSoCFPGAWatchdogReset
	}
	if c.open == nil {
		c.open = openDevice
	}
	if c.start == nil {
		c.start = startHelper
	}
	if c.openNull == nil {
		c.openNull = func() (*os.File, error) { return os.OpenFile("/dev/null", os.O_RDWR, 0) }
	}
	return c
}

// Result reports whether the hardware backstop was armed and the timeout read from it.
type Result struct {
	Armed         *Armed
	ActualTimeout time.Duration
	Reason        string
}

// Armed retains the watchdog descriptor until reboot or Cancel.
type Armed struct {
	mu       sync.Mutex
	device   watchdog
	fallback helper
	marker   string
	canceled bool
}

var retained *Armed
var retainedMu sync.Mutex

// Arm prepares reset and arms a watchdog for an agent-initiated reboot.
// The watchdog is intentionally never pet after the initial keepalive.
func Arm(ctx context.Context, config Config) (Result, error) {
	c := config.defaults()
	if err := ctx.Err(); err != nil {
		return Result{Reason: "context canceled"}, err
	}
	if err := c.prepare(); err != nil {
		return Result{Reason: "reset preparation failed"}, err
	}
	d, err := c.open(c.Device)
	// A just-confirmed trial's guard writes 'V' and closes within one heartbeat.
	// Wait for that bounded handover instead of rebooting without the backstop.
	for waited := time.Duration(0); errors.Is(err, unix.EBUSY) && waited < c.BusyWait; waited += c.BusyPoll {
		c.sleep(c.BusyPoll)
		d, err = c.open(c.Device)
	}
	if errors.Is(err, unix.EBUSY) {
		return Result{Reason: "watchdog busy: trial guard still owns it"}, nil
	}
	if errors.Is(err, unix.ENOENT) {
		return Result{Reason: "watchdog absent"}, nil
	}
	if err != nil {
		return Result{Reason: "watchdog open failed"}, err
	}
	// Every return before a successful arm leaves the watchdog stopped: a
	// "not armed" result must never become a surprise reset later (for
	// example if the reboot request then fails and nothing cancels it).
	closeDevice := true
	defer func() {
		if closeDevice {
			_, _ = io.WriteString(d, "V")
			_ = d.Close()
		}
	}()
	char, err := d.CharDevice()
	if err != nil {
		return Result{Reason: "watchdog stat failed"}, err
	}
	if !char {
		return Result{Reason: "watchdog is not a character device"}, errors.New("watchdog is not a character device")
	}
	actual, err := d.SetTimeout(c.WatchdogTimeout)
	if err != nil {
		return Result{Reason: "watchdog timeout failed"}, err
	}
	result := Result{ActualTimeout: actual}
	if actual < c.MinTimeout {
		result.Reason = "watchdog timeout below minimum"
		return result, fmt.Errorf("watchdog timeout %s below minimum %s", actual, c.MinTimeout)
	}
	if n, err := d.Write([]byte{0}); err != nil || n != 1 {
		result.Reason = "watchdog keepalive failed"
		if err == nil {
			err = io.ErrShortWrite
		}
		return result, err
	}
	marker, err := os.OpenFile(c.Marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		result.Reason = "reboot marker failed"
		return result, fmt.Errorf("create reboot marker: %w", err)
	}
	deadline := time.Now().Add(c.FallbackDelay)
	_, writeErr := fmt.Fprintf(marker, "pid=%d\ndeadline=%s\n", os.Getpid(), deadline.Format(time.RFC3339Nano))
	err = errors.Join(writeErr, marker.Close())
	if err != nil {
		_ = os.Remove(c.Marker)
		result.Reason = "reboot marker failed"
		return result, err
	}
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM HUP INT; sleep "+strconv.FormatInt(int64(c.FallbackDelay/time.Second), 10)+"; exec /sbin/reboot -f")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := c.openNull()
	if err != nil {
		_ = os.Remove(c.Marker)
		result.Reason = "fallback setup failed"
		return result, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	fallback, startErr := c.start(cmd)
	_ = null.Close()
	if startErr != nil {
		_ = os.Remove(c.Marker)
		result.Reason = "fallback start failed"
		return result, fmt.Errorf("start reboot fallback: %w", startErr)
	}
	handle := &Armed{device: d, fallback: fallback, marker: c.Marker}
	retainedMu.Lock()
	retained = handle
	retainedMu.Unlock()
	closeDevice = false
	result.Armed = handle
	result.Reason = "armed"
	return result, nil
}

// Cancel is for a failed reboot request only.
func (a *Armed) Cancel() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.canceled {
		return nil
	}
	a.canceled = true
	var err error
	if a.fallback != nil {
		err = errors.Join(err, a.fallback.KillWait())
	}
	if a.device != nil {
		n, writeErr := io.WriteString(a.device, "V")
		if writeErr == nil && n != 1 {
			writeErr = io.ErrShortWrite
		}
		err = errors.Join(err, writeErr, a.device.Close())
	}
	err = errors.Join(err, os.Remove(a.marker))
	retainedMu.Lock()
	if retained == a {
		retained = nil
	}
	retainedMu.Unlock()
	return err
}

// DisarmStale hands back a watchdog only on a stable boot without a pending backstop.
func DisarmStale(config Config, trial bool) {
	if trial {
		return
	}
	c := config.defaults()
	if _, err := os.Lstat(c.Marker); err == nil {
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("reboot watchdog marker check failed", "error", err)
		return
	}
	d, err := c.open(c.Device)
	if errors.Is(err, unix.EBUSY) || errors.Is(err, unix.ENOENT) {
		return
	}
	if err != nil {
		slog.Warn("stale reboot watchdog open failed", "error", err)
		return
	}
	char, statErr := d.CharDevice()
	if statErr != nil || !char {
		_ = d.Close()
		slog.Warn("stale reboot watchdog is not a character device", "error", statErr)
		return
	}
	n, writeErr := io.WriteString(d, "V")
	if writeErr == nil && n != 1 {
		writeErr = io.ErrShortWrite
	}
	closeErr := d.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		slog.Warn("stale reboot watchdog disarm failed", "error", err)
	} else {
		slog.Info("stale reboot watchdog disarmed")
	}
}
