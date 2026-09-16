//go:build linux

// Package bootlinux contains the Linux mechanisms used by the fixed FES
// bootstrap. Selection, image validation and durable update state belong to
// its caller. These mechanisms never program the FPGA.
package bootlinux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// GuardConfig identifies one consumed trial. Zero durations use 180 seconds
// for the trial, 30 seconds for the device and 5 seconds between heartbeats.
type GuardConfig struct {
	Device            string
	BootID            string
	ImageSHA256       string
	TrialTimeout      time.Duration
	HeartbeatInterval time.Duration
	DeviceTimeout     time.Duration
}

type Confirmation func(context.Context, string, string) (bool, error)

var bootIdentity = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var imageIdentity = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (c GuardConfig) normalized() (GuardConfig, error) {
	if c.Device == "" {
		c.Device = "/dev/watchdog"
	}
	if c.TrialTimeout == 0 {
		c.TrialTimeout = 180 * time.Second
	}
	if c.DeviceTimeout == 0 {
		c.DeviceTimeout = 30 * time.Second
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if !bootIdentity.MatchString(c.BootID) || !imageIdentity.MatchString(c.ImageSHA256) {
		return c, errors.New("watchdog requires exact boot ID and image SHA-256")
	}
	if c.TrialTimeout <= 0 || c.TrialTimeout > 180*time.Second || c.DeviceTimeout < time.Second || c.DeviceTimeout > 180*time.Second || c.HeartbeatInterval <= 0 || c.HeartbeatInterval >= c.DeviceTimeout/2 {
		return c, errors.New("invalid watchdog trial or heartbeat duration")
	}
	return c, nil
}

// StartGuard reexecutes a static bootstrap binary in a separate mount namespace
// and session. The child must call RunGuard with fd 3 as its ready writer.
// Return occurs only after the watchdog is armed and namespace mounts private.
// The caller must retain the executable's bootstrap mount until pivot/exec.
// On failure the child is killed/reaped: if it already opened the watchdog,
// recovery remains armed, so the caller must fail closed and reboot, not boot
// an unguarded trial. ctx bounds startup only, not the child's trial lifetime.
func StartGuard(ctx context.Context, executable string, args []string) (*exec.Cmd, error) {
	return startGuard(ctx, executable, args, &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS, Setsid: true})
}

func startGuard(ctx context.Context, executable string, args []string, attr *syscall.SysProcAttr) (*exec.Cmd, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = attr
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		_ = writer.Close()
		return nil, err
	}
	_ = writer.Close()
	ack := make(chan error, 1)
	go func() {
		var data [6]byte
		_, readErr := io.ReadFull(reader, data[:])
		if readErr == nil && string(data[:]) != "armed\n" {
			readErr = errors.New("invalid watchdog acknowledgement")
		}
		ack <- readErr
	}()
	select {
	case err = <-ack:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("watchdog child did not arm: %w", err)
	}
	return cmd, nil
}

// RunGuard runs only in StartGuard's separate process and mount namespace.
// confirmed must read durable state and return true only when BOTH supplied
// identities match a successfully synced confirmation. It must respect ctx.
// Candidate service shutdown cannot cancel this guard: termination signals
// are ignored, while SIGKILL closes the device without magic and forces reset.
// Never add signal/error cleanup that writes magic V.
func RunGuard(ctx context.Context, cfg GuardConfig, ready io.Writer, confirmed Confirmation) error {
	var err error
	cfg, err = cfg.normalized()
	if err != nil {
		return err
	}
	if ready == nil || confirmed == nil {
		return errors.New("watchdog requires acknowledgement and confirmation callback")
	}
	if err = privateGuardNamespace(); err != nil {
		return err
	}
	signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	fd, err := unix.Open(cfg.Device, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open watchdog: %w", err)
	}
	f := os.NewFile(uintptr(fd), cfg.Device)
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR {
		_ = f.Close()
		return errors.New("watchdog is not a character device")
	}
	return runGuard(ctx, cfg, ready, confirmed, &watchdogFile{f})
}

func privateGuardNamespace() error {
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		return err
	}
	initial, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil {
		return err
	}
	if self == initial {
		return errors.New("watchdog must have its own mount namespace")
	}
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("isolate watchdog mounts: %w", err)
	}
	return nil
}

type watchdog interface {
	io.WriteCloser
	SetTimeout(time.Duration) (time.Duration, error)
}
type watchdogFile struct{ *os.File }

func (d *watchdogFile) SetTimeout(timeout time.Duration) (time.Duration, error) {
	seconds := int((timeout + time.Second - 1) / time.Second)
	if err := unix.IoctlSetPointerInt(int(d.Fd()), unix.WDIOC_SETTIMEOUT, seconds); err != nil {
		return 0, err
	}
	actual, err := unix.IoctlGetInt(int(d.Fd()), unix.WDIOC_GETTIMEOUT)
	return time.Duration(actual) * time.Second, err
}

func writeAll(w io.Writer, p string) error {
	n, err := io.WriteString(w, p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return err
}

func runGuard(ctx context.Context, cfg GuardConfig, ready io.Writer, confirmed Confirmation, d watchdog) (err error) {
	// Ordinary close intentionally does NOT send magic. Even errors after open
	// leave the device active, including a failed timeout ioctl or ready pipe.
	defer func() { err = errors.Join(err, d.Close()) }()
	cfg, err = cfg.normalized()
	if err != nil {
		return err
	}
	timeout, err := d.SetTimeout(cfg.DeviceTimeout)
	if err != nil {
		return fmt.Errorf("set watchdog timeout: %w", err)
	}
	if timeout <= 2*cfg.HeartbeatInterval {
		return errors.New("watchdog timeout is too short for heartbeat")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.TrialTimeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = writeAll(d, "\x00"); err != nil {
		return err
	}
	if err = writeAll(ready, "armed\n"); err != nil {
		return err
	}
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		ok, checkErr := confirmed(ctx, cfg.BootID, cfg.ImageSHA256)
		if checkErr != nil {
			return fmt.Errorf("read durable confirmation: %w", checkErr)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if ok {
			return writeAll(d, "V")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = writeAll(d, "\x00"); err != nil {
				return err
			}
		}
	}
}
