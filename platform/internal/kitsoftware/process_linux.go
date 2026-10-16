package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Keep three worst-case sequential service stops below the appliance watchdog's
// shutdown budget, including init's other service teardown.
const (
	StopTermGrace        = 5 * time.Second
	StopKillGrace        = 2 * time.Second
	StopGroupTimeout     = 22 * time.Second
	StopCommandTimeout   = 24 * time.Second
	AgentConfigPath      = "/media/fat/fogcast/agent.toml"
	AgentConfigWait      = 30 * time.Second
	LauncherProbeTimeout = 10 * time.Second
	roleRuntime          = "runtime"
	roleAgent            = "agent"
	roleLauncher         = "launcher"
	launcherRestartMin   = time.Second
	launcherRestartMax   = 30 * time.Second
	launcherStableLife   = 2 * time.Second
)

// ErrProcessExited reports that a supervised process ended before acceptance.
// Startup may retry it. An accepted runtime or agent exit is a service fault
// and is not restarted behind an execution lease. An accepted launcher exit is
// restarted by RestartExitedLauncher without stopping the other two.
var ErrProcessExited = errors.New("software process exited")

type child struct {
	cmd     *exec.Cmd
	done    chan struct{}
	err     error
	path    string
	args    []string
	env     []string
	role    string
	started time.Time
}

// ProcessController directly parents the kit processes. It never relies on
// shell pid files or reports a successful stop before Wait confirms termination.
type ProcessController struct {
	Resolve             func(context.Context, string) (string, error)
	Admission           *UnixAdmission
	BaseIdentity        string
	PermitUpdateBlocked bool
	RunDir              string
	// AgentConfigPath and AgentConfigWait override the FAT configuration wait.
	// Zero values use AgentConfigPath and AgentConfigWait.
	AgentConfig          string
	AgentConfigWait      time.Duration
	LauncherProbeTimeout time.Duration
	// Output receives child stdout and stderr. Nil uses the supervisor stdout.
	Output          io.Writer
	mu              sync.Mutex
	children        []*child
	active          string
	prepared        *preparedRelease
	launcherRestart time.Time
	launcherDelay   time.Duration
}

func (c *ProcessController) Active() string { c.mu.Lock(); defer c.mu.Unlock(); return c.active }
func (c *ProcessController) Verify(ctx context.Context, id string) error {
	prepared, err := c.prepare(ctx, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prepared = prepared // A failed verification invalidates every older preparation.
	return err
}
func (c *ProcessController) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopLocked(ctx)
}
func (c *ProcessController) stopLocked(ctx context.Context) error {
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
func (c *ProcessController) start(path string, args []string, env []string, role string) error {
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), env...)
	output := c.Output
	if output == nil {
		output = os.Stdout
	}
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return err
	}
	p := &child{
		cmd:     cmd,
		done:    make(chan struct{}),
		path:    path,
		args:    append([]string(nil), args...),
		env:     append([]string(nil), env...),
		role:    role,
		started: time.Now(),
	}
	c.children = append(c.children, p)
	pid := cmd.Process.Pid
	go func() {
		p.err = cmd.Wait()
		name := filepath.Base(path)
		if role != "" {
			name = role + " " + name
		}
		log.Printf("software process %s pid %d exited: %v", name, pid, p.err)
		close(p.done)
	}()
	return nil
}
func (c *ProcessController) Start(ctx context.Context, id string, r Request) error {
	// Factory trial startup calls Start directly. Engine paths always prepare
	// before their bounded stop/start window; retries reuse that preparation.
	c.mu.Lock()
	missing := c.prepared == nil || c.prepared.id != id
	c.mu.Unlock()
	if missing {
		if err := c.Verify(ctx, id); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.children) != 0 {
		return errors.New("old software processes have not been stopped")
	}
	root, err := c.preparedPath(ctx, id)
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
	configPath := c.AgentConfig
	if configPath == "" {
		configPath = AgentConfigPath
	}
	configWait := c.AgentConfigWait
	if configWait <= 0 {
		configWait = AgentConfigWait
	}
	if err := waitForAgentConfig(ctx, configPath, configWait); err != nil {
		return err
	}
	if err := c.start(filepath.Join(root, "bin/mister-runtime"), []string{"--menu-package", "/usr/share/mister-runtime/core-packages/" + menuID, menuID}, env, roleRuntime); err != nil {
		return err
	}
	// Native initialization owns FPGA programming and completes before its
	// protocol socket opens. Preserve diagnostic services after a completed
	// physical failure; Healthy still requires native idle before acceptance.
	if err := c.startAgentAfterNativeInitialization(ctx, c.children[len(c.children)-1],
		filepath.Join(root, "bin/mister-agent"), []string{"--config", configPath}, env,
		"/run/mister-runtime.sock", readNativeStatus); err != nil {
		return err
	}
	catalog := filepath.Join(root, "share/fogcast/config.toml")
	kit := filepath.Join(root, "bin/fogcast-kit")
	probeTimeout := c.LauncherProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = LauncherProbeTimeout
	}
	output, err := probeLauncherUI(ctx, kit, probeTimeout)
	if err != nil {
		_ = c.stopLocked(ctx)
		return err
	}
	if output == "tenfoot" {
		err = c.start(filepath.Join(root, "bin/fogcast-tenfoot"), []string{"-config", "/media/fat/fogcast/launcher.json", "-catalog-config", catalog, "-gfx", "menu-display", "-menu-socket", "/run/mister-runtime.sock", "-input", "auto", "-home", "rooms"}, env, roleLauncher)
	} else {
		err = c.start(kit, []string{"--config", "/media/fat/fogcast/launcher.json", "--catalog-config", catalog, "--menu-display"}, env, roleLauncher)
	}
	if err != nil {
		_ = c.stopLocked(ctx)
		return err
	}
	c.active = id
	return nil
}

// nativeStatusProbe is injected only by in-package tests. Production always
// authenticates the socket peer to the child PID, start time and executable.
type nativeStatusProbe func(context.Context, string, int, string, string) (nativeStatus, error)

// Called with c.mu held. No new timeout is created: native startup and the
// remaining service group share the Engine's one startup deadline. A failure
// leaves the child owned and admission fenced for the Engine's recovery path.
func (c *ProcessController) startAgentAfterNativeInitialization(ctx context.Context, runtime *child, path string, args, env []string, socket string, probe nativeStatusProbe) error {
	if err := waitNativeInitialization(ctx, runtime, socket, probe); err != nil {
		return err
	}
	if err := c.start(path, args, env, roleAgent); err != nil {
		_ = c.stopLocked(ctx)
		return err
	}
	return nil
}

func waitNativeInitialization(ctx context.Context, runtime *child, socket string, probe nativeStatusProbe) error {
	identity, err := processStartTime(runtime.cmd.Process.Pid)
	if err != nil {
		return fmt.Errorf("runtime startup process unavailable: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-runtime.done:
			return fmt.Errorf("runtime exited during native initialization: %w", ErrProcessExited)
		default:
		}
		status, err := probe(ctx, socket, runtime.cmd.Process.Pid, identity, runtime.path)
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		select {
		case <-runtime.done:
			return fmt.Errorf("runtime exited during native initialization: %w", ErrProcessExited)
		default:
		}
		if err == nil {
			idle := status.State == "idle" && status.Error == nil
			failed := status.State == "reboot_required" && status.Error != nil && status.Error.Code == "idle_failed" && status.Error.Phase == "recovery"
			if !status.OK || status.Execution != "none" || (!idle && !failed) {
				return fmt.Errorf("runtime native initialization is not complete: state=%s execution=%s error=%v", status.State, status.Execution, status.Error)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("runtime startup status failed: %w", err)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-runtime.done:
			timer.Stop()
			return fmt.Errorf("runtime exited during native initialization: %w", ErrProcessExited)
		case <-timer.C:
		}
	}
}

func waitForAgentConfig(ctx context.Context, path string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("mister-agent: writable FAT configuration did not appear within %s", wait)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func probeLauncherUI(ctx context.Context, kit string, timeout time.Duration) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	probe := exec.CommandContext(probeCtx, kit, "--config", "/media/fat/fogcast/launcher.json", "--print-kit-ui")
	// A descendant that inherits the output pipe must not extend the probe
	// past this deadline after the launcher process itself is killed.
	probe.WaitDelay = timeout
	output, err := probe.Output()
	if err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return "", fmt.Errorf("read launcher selection: timed out after %s", timeout)
		}
		return "", fmt.Errorf("read launcher selection: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
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
			return fmt.Errorf("%s exited: %v: %w", p.path, p.err, ErrProcessExited)
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

// ServiceFault does not restart an accepted runtime or agent behind an
// execution owner's back. A dead launcher is reported while RestartExitedLauncher
// brings that one process back. Operators can inspect this through the base
// supervisor's local socket.
func (c *ProcessController) ServiceFault() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.children {
		select {
		case <-p.done:
			if p.role == roleLauncher && launcherPeersAlive(c.children) {
				return fmt.Sprintf("%s exited: %v; supervisor is restarting the launcher", filepath.Base(p.path), p.err)
			}
			return fmt.Sprintf("%s exited: %v; explicit supervisor restart required", filepath.Base(p.path), p.err)
		default:
		}
	}
	if c.active == "" {
		return "kit software services are not running"
	}
	return ""
}

func launcherPeersAlive(children []*child) bool {
	if len(children) != 3 {
		return false
	}
	for _, p := range children[:2] {
		if p.role != roleRuntime && p.role != roleAgent {
			return false
		}
		select {
		case <-p.done:
			return false
		default:
		}
	}
	return true
}

// RestartExitedLauncher replaces a dead accepted launcher. The runtime and
// agent stay up, so an execution lease is not dropped and the menu can return.
// A dead runtime or agent still requires an explicit supervisor restart.
// Called with no controller lock held.
func (c *ProcessController) RestartExitedLauncher() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == "" || !launcherPeersAlive(c.children) {
		return nil
	}
	launcher := c.children[2]
	if launcher.role != roleLauncher {
		return nil
	}
	select {
	case <-launcher.done:
	default:
		if time.Since(launcher.started) >= launcherStableLife {
			c.launcherDelay = launcherRestartMin
		}
		return nil
	}
	delay := c.launcherDelay
	if delay < launcherRestartMin {
		delay = launcherRestartMin
	}
	if !c.launcherRestart.IsZero() && time.Since(c.launcherRestart) < delay {
		return nil
	}
	log.Printf("restarting launcher after %s exited: %v", filepath.Base(launcher.path), launcher.err)
	lived := time.Since(launcher.started)
	c.children = c.children[:2]
	if err := c.start(launcher.path, launcher.args, launcher.env, roleLauncher); err != nil {
		c.children = append(c.children, launcher)
		return err
	}
	c.launcherRestart = time.Now()
	if lived < launcherStableLife {
		if c.launcherDelay < launcherRestartMin {
			c.launcherDelay = launcherRestartMin
		} else if c.launcherDelay < launcherRestartMax {
			c.launcherDelay *= 2
		}
	} else {
		c.launcherDelay = launcherRestartMin
	}
	if err := os.Remove(filepath.Join(c.RunDir, "launcher-ready.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
