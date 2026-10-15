package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Keep three worst-case sequential service stops below the appliance watchdog's
// shutdown budget, including init's other service teardown.
const (
	StopTermGrace      = 5 * time.Second
	StopKillGrace      = 2 * time.Second
	StopGroupTimeout   = 22 * time.Second
	StopCommandTimeout = 24 * time.Second
)

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	path string
}

// ProcessController directly parents the kit processes. It never relies on
// shell pid files or reports a successful stop before Wait confirms termination.
type ProcessController struct {
	Resolve             func(context.Context, string) (string, error)
	Admission           *UnixAdmission
	BaseIdentity        string
	PermitUpdateBlocked bool
	RunDir              string
	mu                  sync.Mutex
	children            []*child
	active              string
}

func (c *ProcessController) Active() string { c.mu.Lock(); defer c.mu.Unlock(); return c.active }
func (c *ProcessController) Verify(ctx context.Context, id string) error {
	_, err := c.Resolve(ctx, id)
	return err
}
func (c *ProcessController) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.children) - 1; i >= 0; i-- {
		p := c.children[i]
		select {
		case <-p.done:
		default:
			if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
			timer := time.NewTimer(StopTermGrace)
			select {
			case <-p.done:
				timer.Stop()
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					return err
				}
				timer = time.NewTimer(StopKillGrace)
				select {
				case <-p.done:
					timer.Stop()
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
					return fmt.Errorf("process %d has not exited", p.cmd.Process.Pid)
				}
			}
		}
		// Descendants share the dedicated process group; a surviving descendant is
		// equally unsafe even after the direct executable was reaped.
		if err := syscall.Kill(-p.cmd.Process.Pid, 0); err == nil {
			return fmt.Errorf("process group %d still exists", p.cmd.Process.Pid)
		} else if !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	c.children = nil
	c.active = ""
	return os.RemoveAll(filepath.Join(c.RunDir, "launcher-ready.json"))
}
func (c *ProcessController) start(path string, args []string, env []string) error {
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return err
	}
	p := &child{cmd: cmd, done: make(chan struct{}), path: path}
	c.children = append(c.children, p)
	go func() { p.err = cmd.Wait(); close(p.done) }()
	return nil
}
func (c *ProcessController) Start(ctx context.Context, id string, r Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.children) != 0 {
		return errors.New("old software processes have not been stopped")
	}
	root, err := c.Resolve(ctx, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.RunDir, 0700); err != nil {
		return err
	}
	current := filepath.Join(c.RunDir, "current")
	if err := os.Remove(current + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Symlink(root, current+".tmp"); err != nil {
		return err
	}
	if err := os.Rename(current+".tmp", current); err != nil {
		return err
	}
	ready := filepath.Join(c.RunDir, "launcher-ready.json")
	if err := os.Remove(ready); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	claim, err := json.Marshal(c.Admission.Claim(r))
	if err != nil {
		return err
	}
	env := []string{"FES_SOFTWARE_MANAGED=1", "FES_SOFTWARE_BUNDLE_ID=" + id, "FES_SOFTWARE_BASE_IDENTITY=" + c.BaseIdentity, "FES_SOFTWARE_READY_FILE=" + ready}
	if r.ID != "" {
		env = append(env, "FES_SOFTWARE_REQUEST="+string(claim))
	}
	// Idle/menu core packages belong to the OS/core library, not the userspace
	// bundle. Preserve their explicit immutable selection.
	selection, err := os.ReadFile("/usr/share/mister-runtime/selections/fes-menu.package.toml")
	if err != nil {
		return fmt.Errorf("software supervisor requires factory menu selection: %w", err)
	}
	menuID := ""
	for _, line := range strings.Split(string(selection), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "package_id" {
			menuID = strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			break
		}
	}
	if len(menuID) != 64 || strings.Trim(menuID, "0123456789abcdef") != "" {
		return errors.New("invalid menu package selection")
	}
	if err := c.start(filepath.Join(root, "bin/mister-runtime"), []string{"--menu-package", "/usr/share/mister-runtime/core-packages/" + menuID, menuID}, env); err != nil {
		return err
	}
	if err := c.start(filepath.Join(root, "bin/mister-agent"), []string{"--config", "/media/fat/fogcast/agent.toml"}, env); err != nil {
		return err
	}
	catalog := filepath.Join(root, "share/fogcast/config.toml")
	kit := filepath.Join(root, "bin/fogcast-kit")
	probe := exec.CommandContext(ctx, kit, "--config", "/media/fat/fogcast/launcher.json", "--print-kit-ui")
	output, err := probe.Output()
	if err != nil {
		return fmt.Errorf("read launcher selection: %w", err)
	}
	if strings.TrimSpace(string(output)) == "tenfoot" {
		err = c.start(filepath.Join(root, "bin/fogcast-tenfoot"), []string{"-config", "/media/fat/fogcast/launcher.json", "-catalog-config", catalog, "-gfx", "menu-display", "-menu-socket", "/run/mister-runtime.sock", "-input", "auto", "-home", "rooms"}, env)
	} else {
		err = c.start(kit, []string{"--config", "/media/fat/fogcast/launcher.json", "--catalog-config", catalog, "--menu-display"}, env)
	}
	if err != nil {
		return err
	}
	c.active = id
	return nil
}
func (c *ProcessController) Healthy(ctx context.Context, id string) error {
	c.mu.Lock()
	children := append([]*child(nil), c.children...)
	active := c.active
	c.mu.Unlock()
	if active != id || len(children) != 3 {
		return errors.New("software processes incomplete")
	}
	for _, p := range children {
		select {
		case <-p.done:
			return fmt.Errorf("%s exited: %v", p.path, p.err)
		default:
		}
		actual, err := os.Stat("/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/exe")
		if err != nil {
			return err
		}
		expected, err := os.Stat(p.path)
		if err != nil {
			return err
		}
		if !os.SameFile(actual, expected) {
			return errors.New("running executable differs from selected bundle")
		}
	}
	s, err := c.Admission.Status(ctx)
	if err != nil {
		return err
	}
	if s.BundleID != id || !s.RawIdleReady || (s.UpdateBlocked && !c.PermitUpdateBlocked) {
		return errors.New("agent/native readiness is not compatible idle")
	}
	b, err := os.ReadFile(filepath.Join(c.RunDir, "launcher-ready.json"))
	if err != nil {
		return err
	}
	var ready struct {
		BundleID string `json:"bundle_id"`
		PID      int    `json:"pid"`
	}
	if err := json.Unmarshal(b, &ready); err != nil {
		return err
	}
	if ready.BundleID != id || ready.PID != children[2].cmd.Process.Pid {
		return errors.New("launcher readiness does not identify selected process")
	}
	return nil
}

// StopOrphans runs once under the supervisor singleton lock. Pdeathsig terminates
// direct children if the supervisor dies; scan the explicit managed-process
// marker as well so startup never races a still-exiting old runtime.
func StopOrphans(ctx context.Context) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		identity, err := processStartTime(pid)
		if err != nil {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil {
			continue
		}
		managed := false
		for _, value := range strings.Split(string(environ), "\x00") {
			if value == "FES_SOFTWARE_MANAGED=1" {
				managed = true
			}
		}
		if !managed {
			continue
		}
		// pidfd pins the process identity; PID reuse cannot redirect this signal.
		fd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return err
		}
		current, err := processStartTime(pid)
		if err != nil || current != identity {
			unix.Close(fd)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return errors.New("managed process identity changed before stop")
		}
		if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			unix.Close(fd)
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			n, e := unix.Poll(fds, 100)
			if e != nil && !errors.Is(e, unix.EINTR) {
				unix.Close(fd)
				return e
			}
			if n > 0 {
				break
			}
			if ctx.Err() != nil {
				unix.Close(fd)
				return ctx.Err()
			}
			if time.Now().After(deadline) {
				unix.Close(fd)
				return fmt.Errorf("previous process %d did not exit", pid)
			}
		}
		unix.Close(fd)
	}
	return nil
}

// ServiceFault does not restart accepted software behind an execution owner's
// back. Operators can inspect this through the base supervisor's local socket.
func (c *ProcessController) ServiceFault() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.children {
		select {
		case <-p.done:
			return fmt.Sprintf("%s exited: %v; explicit supervisor restart required", filepath.Base(p.path), p.err)
		default:
		}
	}
	if c.active == "" {
		return "kit software services are not running"
	}
	return ""
}

func processStartTime(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	close := strings.LastIndex(string(b), ")")
	if close < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(b)[close+1:])
	if len(fields) < 20 {
		return "", errors.New("short process stat")
	}
	return fields[19], nil
}

// CheckNoManagedProcesses is a final read-only confirmation after the supervisor
// exits. A failed stop cannot be disguised by deleting its PID file.
func CheckNoManagedProcesses() error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, value := range strings.Split(string(environ), "\x00") {
			if value == "FES_SOFTWARE_MANAGED=1" {
				return fmt.Errorf("managed software process %d survived shutdown", pid)
			}
		}
	}
	return nil
}
