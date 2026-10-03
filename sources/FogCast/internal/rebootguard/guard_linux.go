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
	// MinTimeout is the smallest read-back watchdog timeout that is kept. It
	// defaults to FallbackMinDeadline + FallbackSyncWait + WatchdogMargin.
	MinTimeout time.Duration
	// The fallback forces `reboot -f` after FallbackStall seconds without
	// block-device I/O progress, and at FallbackDeadline at the latest, after a
	// bounded explicit sync of FallbackSyncWait. When the watchdog is armed the
	// fallback deadline is shortened to end WatchdogMargin before the watchdog,
	// but never below FallbackMinDeadline (otherwise the watchdog is skipped).
	FallbackStall       time.Duration
	FallbackDeadline    time.Duration
	FallbackMinDeadline time.Duration
	FallbackSyncWait    time.Duration
	FallbackPoll        time.Duration
	WatchdogMargin      time.Duration
	Marker              string
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

// KillWait kills the helper and reaps it for at most two seconds, so Cancel
// can never block on an unreapable helper.
func (p process) KillWait() error {
	err := p.Kill()
	reaped := make(chan error, 1)
	go func() { _, waitErr := p.Wait(); reaped <- waitErr }()
	select {
	case waitErr := <-reaped:
		return errors.Join(err, waitErr)
	case <-time.After(2 * time.Second):
		return errors.Join(err, errors.New("reboot fallback helper not reaped within 2s"))
	}
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
	if c.FallbackStall == 0 {
		c.FallbackStall = 30 * time.Second
	}
	if c.FallbackDeadline == 0 {
		c.FallbackDeadline = 150 * time.Second
	}
	if c.FallbackPoll == 0 {
		c.FallbackPoll = 5 * time.Second
	}
	if c.FallbackMinDeadline == 0 {
		c.FallbackMinDeadline = 60 * time.Second
	}
	if c.FallbackSyncWait == 0 {
		c.FallbackSyncWait = 10 * time.Second
	}
	if c.WatchdogMargin == 0 {
		c.WatchdogMargin = 20 * time.Second
	}
	if c.MinTimeout == 0 {
		// The watchdog is the final deadline: it must fire strictly after the
		// shortest fallback (deadline + bounded sync) plus margin for reboot -f.
		c.MinTimeout = c.FallbackMinDeadline + c.FallbackSyncWait + c.WatchdogMargin
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

// Result reports which backstop layers are in place for this reboot. Armed is
// non-nil whenever any layer (fallback or watchdog) needs Cancel on failure.
type Result struct {
	Armed            *Armed
	Fallback         bool
	Watchdog         bool
	ActualTimeout    time.Duration
	FallbackDeadline time.Duration
	Reason           string
}

// Armed retains the fallback helper and watchdog descriptor until reboot or Cancel.
type Armed struct {
	mu       sync.Mutex
	device   watchdog
	fallback helper
	marker   string
	canceled bool
}

var retained *Armed
var retainedMu sync.Mutex

// Arm installs the reboot backstops for an agent-initiated reboot. The delayed
// reboot -f fallback is started independently of the watchdog, so it remains in
// place whenever the watchdog cannot be armed. The watchdog is intentionally
// never pet after its initial keepalive. The returned error describes only a
// layer that could not be installed; the caller reboots regardless.
func Arm(ctx context.Context, config Config) (Result, error) {
	c := config.defaults()
	if err := ctx.Err(); err != nil {
		return Result{Reason: "context canceled"}, err
	}
	handle := &Armed{}
	var result Result
	var errs []error
	if err := createMarker(c); err != nil {
		errs = append(errs, err)
	} else {
		handle.marker = c.Marker
	}
	device, actual, reason, err := armWatchdog(c)
	result.ActualTimeout = actual
	if err != nil {
		errs = append(errs, err)
	}
	if device != nil {
		handle.device = device
		result.Watchdog = true
	}
	result.FallbackDeadline = fallbackDeadline(c, device != nil, actual)
	if fallback, err := startFallback(c, result.FallbackDeadline); err != nil {
		errs = append(errs, fmt.Errorf("start reboot fallback: %w", err))
	} else {
		handle.fallback = fallback
		result.Fallback = true
	}
	switch {
	case result.Watchdog && result.Fallback:
		result.Reason = "armed"
	case result.Fallback:
		result.Reason = "fallback only: " + reason
	case result.Watchdog:
		result.Reason = "watchdog only: fallback failed"
	default:
		result.Reason = "no backstop: " + reason
	}
	if handle.device != nil || handle.fallback != nil || handle.marker != "" {
		retainedMu.Lock()
		retained = handle
		retainedMu.Unlock()
		result.Armed = handle
	}
	return result, errors.Join(errs...)
}

// fallbackDeadline keeps the fallback (deadline + bounded sync) ending at least
// WatchdogMargin before an armed watchdog. armWatchdog only keeps a watchdog
// whose timeout leaves room for FallbackMinDeadline, so the result never drops
// below it.
func fallbackDeadline(c Config, watchdogArmed bool, actual time.Duration) time.Duration {
	deadline := c.FallbackDeadline
	if watchdogArmed {
		if limit := actual - c.FallbackSyncWait - c.WatchdogMargin; limit < deadline {
			deadline = limit
		}
	}
	return deadline
}

func createMarker(c Config) error {
	marker, err := os.OpenFile(c.Marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create reboot marker: %w", err)
	}
	deadline := time.Now().Add(c.FallbackDeadline + c.FallbackSyncWait)
	_, writeErr := fmt.Fprintf(marker, "pid=%d\ndeadline=%s\n", os.Getpid(), deadline.Format(time.RFC3339Nano))
	if err = errors.Join(writeErr, marker.Close()); err != nil {
		_ = os.Remove(c.Marker)
		return fmt.Errorf("write reboot marker: %w", err)
	}
	return nil
}

func startFallback(c Config, deadline time.Duration) (helper, error) {
	seconds := func(d time.Duration) string { return strconv.FormatInt(int64(d/time.Second), 10) }
	cmd := exec.Command("/bin/sh", "-c", fallbackScript, "fes-reboot-fallback",
		seconds(c.FallbackStall), seconds(deadline), seconds(c.FallbackPoll), "/proc", "/sbin/reboot", seconds(c.FallbackSyncWait))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := c.openNull()
	if err != nil {
		return nil, err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	return c.start(cmd)
}

// armWatchdog returns an armed device or nil. Every path that does not return
// a device leaves an opened watchdog magic-closed (stopped), so a skipped
// watchdog can never become a surprise reset later.
func armWatchdog(c Config) (watchdog, time.Duration, string, error) {
	if err := c.prepare(); err != nil {
		return nil, 0, "reset preparation failed", err
	}
	d, err := c.open(c.Device)
	// A just-confirmed trial's guard writes 'V' and closes within one heartbeat.
	// Wait for that bounded handover instead of rebooting without the watchdog.
	for waited := time.Duration(0); errors.Is(err, unix.EBUSY) && waited < c.BusyWait; waited += c.BusyPoll {
		c.sleep(c.BusyPoll)
		d, err = c.open(c.Device)
	}
	if errors.Is(err, unix.EBUSY) {
		return nil, 0, "watchdog busy: trial guard still owns it", nil
	}
	if errors.Is(err, unix.ENOENT) {
		return nil, 0, "watchdog absent", nil
	}
	if err != nil {
		return nil, 0, "watchdog open failed", err
	}
	disarm := func() {
		_, _ = io.WriteString(d, "V")
		_ = d.Close()
	}
	char, err := d.CharDevice()
	if err != nil || !char {
		disarm()
		return nil, 0, "watchdog is not a character device", errors.Join(err, errors.New("watchdog is not a character device"))
	}
	actual, err := d.SetTimeout(c.WatchdogTimeout)
	if err != nil {
		disarm()
		return nil, 0, "watchdog timeout failed", err
	}
	if actual < c.MinTimeout {
		disarm()
		return nil, actual, "watchdog timeout below minimum", fmt.Errorf("watchdog timeout %s below minimum %s", actual, c.MinTimeout)
	}
	if n, err := d.Write([]byte{0}); err != nil || n != 1 {
		disarm()
		if err == nil {
			err = io.ErrShortWrite
		}
		return nil, actual, "watchdog keepalive failed", err
	}
	return d, actual, "armed", nil
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
	// Disarm the watchdog first: nothing below may delay stopping it.
	if a.device != nil {
		n, writeErr := io.WriteString(a.device, "V")
		if writeErr == nil && n != 1 {
			writeErr = io.ErrShortWrite
		}
		err = errors.Join(err, writeErr, a.device.Close())
	}
	if a.fallback != nil {
		err = errors.Join(err, a.fallback.KillWait())
	}
	if a.marker != "" {
		err = errors.Join(err, os.Remove(a.marker))
	}
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
