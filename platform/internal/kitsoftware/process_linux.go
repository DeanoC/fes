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
	StopTermGrace      = 5 * time.Second
	StopKillGrace      = 2 * time.Second
	StopGroupTimeout   = 22 * time.Second
	StopCommandTimeout = 24 * time.Second
	AgentConfigPath    = "/media/fat/fogcast/agent.toml"
	AgentConfigWait    = 30 * time.Second
	// PersistentStateDir is the writable FAT directory for kit userspace
	// state. launcher.json, agent.toml and target-id stay beside it. The
	// supervisor publishes this path as XDG_CONFIG_HOME and XDG_STATE_HOME.
	// Tenfoot stores FogCast/tenfoot.json and FogCast/rooms/<id>.state.json
	// underneath it. It is not one of the FESDATA3 bind mounts.
	PersistentStateDir = "/media/fat/fogcast/state"
	roleRuntime        = "runtime"
	roleAgent          = "agent"
	roleLauncher       = "launcher"
	launcherRestartMin = time.Second
	launcherRestartMax = 30 * time.Second
	launcherStableLife = 2 * time.Second
	// groupRestartTimeout bounds one accepted-group start. Native initialization
	// shares this deadline; the supervisor tick retries with backoff after it.
	groupRestartTimeout = 60 * time.Second
	// groupReadyStable is the uninterrupted agent, runtime and launcher
	// readiness window. It is the same interval Engine.health requires before
	// an activation is accepted.
	groupReadyStable = 10 * time.Second
	// groupReadyBudget is how long a replacement may stay up without becoming
	// ready before the supervisor stops it and retries with backoff. A runtime
	// that is already executing a session is ready, so this budget does not
	// stop that group.
	groupReadyBudget = 60 * time.Second
	// launcherGroupReapPoll is how long one restart attempt waits for init to
	// reap a just-signaled descendant. The supervisor tick retries after that.
	launcherGroupReapPoll = 200 * time.Millisecond
)

// ErrProcessExited reports that a supervised process ended before acceptance.
// Startup may retry it. After acceptance, RecoverAcceptedExits restarts a dead
// launcher alone and restarts the whole group when the runtime or agent exits.
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
	AgentConfig     string
	AgentConfigWait time.Duration
	// Output receives child stdout and stderr. Nil uses the supervisor stdout.
	Output          io.Writer
	mu              sync.Mutex
	children        []*child
	active          string
	prepared        *preparedRelease
	launcherRestart time.Time
	launcherDelay   time.Duration
	groupRestart    time.Time
	groupDelay      time.Duration
	// pendingGroup is the accepted bundle waiting to be started again after an
	// agent or runtime exit. It survives Stop, which clears active, and stays
	// set until the replacement has been ready.
	pendingGroup string
	// reaps tracks SIGTERM and SIGKILL deadlines per exited process group.
	// Probing one group must not reset another group's deadline.
	reaps map[int]groupReap
	// recoveryID is the bundle whose replacement is waiting for readiness.
	// recoveryPIDs are the processes that belong to that attempt. A later
	// activation that replaces those processes ends the wait.
	recoveryID      string
	recoveryPIDs    []int
	groupReadyBy    time.Time
	groupReadySince time.Time
	// readyStable and readyBudget override the readiness window in tests.
	readyStable time.Duration
	readyBudget time.Duration
	// holdRecoveryClaims is set only while a group restart is inside Start.
	// The replacement agent closes execution claims before it serves, and Start
	// confirms that fence before the launcher probe.
	holdRecoveryClaims bool
}

type groupReap struct {
	term time.Time
	kill time.Time
}

// groupRestartStart launches a replacement group after an accepted agent or
// runtime exit. request is a durable queued activation, or the zero request
// when none is queued. The zero request does not replay the boot credential.
// Tests replace it.
var groupRestartStart = func(c *ProcessController, ctx context.Context, id string, request Request) error {
	startCtx, cancel := context.WithTimeout(ctx, groupRestartTimeout)
	defer cancel()
	return c.Start(startCtx, id, request)
}

// errRecoveryExecuting means the replacement processes are up and the runtime
// is already in a session. That is a live game, not a failed start.
var errRecoveryExecuting = errors.New("recovered runtime is executing")

// groupReadyProbe is one readiness sample for a replacement group. The default
// is Healthy, which requires agent and native idle plus the launcher readiness
// file. When that fails because a session is already running, the sample is
// errRecoveryExecuting. Tests replace it.
var groupReadyProbe = func(c *ProcessController, ctx context.Context, id string) error {
	err := c.Healthy(ctx, id)
	if err == nil || errors.Is(err, ErrProcessExited) {
		return err
	}
	if recoveryExecution(c, ctx, id) {
		return errRecoveryExecuting
	}
	return err
}

// closeRecoveryClaims blocks new execution claims while a replacement is still
// waiting to be accepted. Without a queued activation the agent's own admission
// fence is open, so a game could start before the idle window finishes and
// make RawIdleReady false. The fence opens only once the group is accepted.
// A nil admission is a test controller. Tests replace this.
var closeRecoveryClaims = func(c *ProcessController, ctx context.Context) error {
	if c == nil || c.Admission == nil {
		return nil
	}
	return c.Admission.FenceExecution(ctx)
}

// openRecoveryClaims allows new execution claims after recovery is accepted.
// An existing session is not revoked. Tests replace this.
var openRecoveryClaims = func(c *ProcessController, ctx context.Context) error {
	if c == nil || c.Admission == nil {
		return nil
	}
	return c.Admission.ClearExecutionFence(ctx)
}

// recoveryExecution reports a live runtime session for this replacement.
// Tests replace it.
var recoveryExecution = func(c *ProcessController, ctx context.Context, id string) bool {
	return c.runtimeExecuting(ctx, id)
}

// readRecoveryStatus is the runtime status read used to recognize a session.
// Tests replace it.
var readRecoveryStatus = readNativeStatus

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

// overlayEnv returns base plus overlay, with overlay keys replacing the
// same names from base. Explicit supervisor settings such as
// XDG_CONFIG_HOME then win over the inherited HOME=/ environment.
func overlayEnv(base []string, overlay ...string) []string {
	drop := make(map[string]struct{}, len(overlay))
	for _, kv := range overlay {
		key, _, ok := strings.Cut(kv, "=")
		if ok {
			drop[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+len(overlay))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if ok {
			if _, skip := drop[key]; skip {
				continue
			}
		}
		out = append(out, kv)
	}
	return append(out, overlay...)
}

func (c *ProcessController) start(path string, args []string, env []string, role string) error {
	cmd := exec.Command(path, args...)
	cmd.Env = overlayEnv(os.Environ(), env...)
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
	if c.holdRecoveryClaims {
		// The agent closes its execution fence before listening. A later
		// supervisor sample confirms the same fence; this env is what closes
		// it before the first claim can be served.
		env = append(env, "FES_SOFTWARE_RECOVERY_FENCE=1")
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
	// Create the state directory only when its FAT parent already exists.
	// MkdirAll would otherwise create /media/fat on a host that reached
	// this line. A failure is logged and the group still starts; tenfoot
	// keeps running and logs its own write failure once.
	stateParent := filepath.Dir(PersistentStateDir)
	if st, err := os.Stat(stateParent); err != nil || !st.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is not a directory", stateParent)
		}
		log.Printf("software state directory %s: parent unavailable: %v", PersistentStateDir, err)
	} else if err := os.MkdirAll(PersistentStateDir, 0o755); err != nil {
		log.Printf("software state directory %s: %v", PersistentStateDir, err)
	}
	env = append(env, "XDG_CONFIG_HOME="+PersistentStateDir, "XDG_STATE_HOME="+PersistentStateDir)
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
	// Confirm the fence before the launcher probe. A claim that lands during
	// that probe would be stopped with the group if the probe fails.
	if c.holdRecoveryClaims {
		if err := c.confirmRecoveryClaims(ctx); err != nil {
			_ = c.stopLocked(ctx)
			return err
		}
	}
	catalog := filepath.Join(root, "share/fogcast/config.toml")
	if err := c.start(filepath.Join(root, "bin/fogcast-tenfoot"), []string{"-config", "/media/fat/fogcast/launcher.json", "-catalog-config", catalog, "-gfx", "menu-display", "-menu-socket", "/run/mister-runtime.sock", "-input", "auto", "-home", "rooms"}, env, roleLauncher); err != nil {
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

// ServiceFault reports an accepted exit while RecoverAcceptedExits brings the
// launcher or the whole group back. Operators can inspect this through the base
// supervisor's local socket.
func (c *ProcessController) ServiceFault() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingGroup != "" {
		return "supervisor is restarting the service group"
	}
	for _, p := range c.children {
		select {
		case <-p.done:
			if p.role == roleLauncher && launcherPeersAlive(c.children) {
				return fmt.Sprintf("%s exited: %v; supervisor is restarting the launcher", filepath.Base(p.path), p.err)
			}
			if p.role == roleAgent || p.role == roleRuntime {
				return fmt.Sprintf("%s exited: %v; supervisor is restarting the service group", filepath.Base(p.path), p.err)
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
// RecoverAcceptedExits is the supervisor entry point. It uses this when both
// peers are alive, and restarts the whole group when the agent or runtime exits.
// The exited launcher's process group is terminated and confirmed gone before
// the replacement starts. If that group cannot be cleared, the launcher stays
// owned so Stop refuses activation and the next call retries.
// Called with no controller lock held.
func (c *ProcessController) RestartExitedLauncher() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.restartExitedLauncherLocked()
}

func (c *ProcessController) restartExitedLauncherLocked() error {
	if c.active == "" || !launcherPeersAlive(c.children) {
		// A dead runtime must not abandon descendants of the launcher that
		// already exited. Keep ownership and retry until that group is gone.
		if launcher := exitedLauncher(c.children); launcher != nil {
			if err := c.confirmLauncherGroup(launcher); err != nil {
				return err
			}
		}
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
		c.clearLauncherReap()
		return nil
	}
	if err := c.confirmLauncherGroup(launcher); err != nil {
		return err
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
	c.clearLauncherReap()
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

func exitedLauncher(children []*child) *child {
	for i := len(children) - 1; i >= 0; i-- {
		p := children[i]
		if p.role != roleLauncher {
			continue
		}
		select {
		case <-p.done:
			return p
		default:
		}
	}
	return nil
}

func (c *ProcessController) clearLauncherReap() {
	c.reaps = nil
}

func (c *ProcessController) clearReap(pid int) {
	delete(c.reaps, pid)
}

// confirmLauncherGroup signals an exited child's process group and returns nil
// only after that group is gone. The direct child is already reaped; descendants
// that inherited the group are not visible to a later Stop once this child is
// dropped. Launcher and group restarts both use it. Each process group keeps
// its own term and kill deadline, so a group that is already gone does not
// restart the clock on a descendant that is still alive.
func (c *ProcessController) confirmLauncherGroup(p *child) error {
	pid := p.cmd.Process.Pid
	if pid <= 1 {
		return fmt.Errorf("invalid process group %d", pid)
	}
	err := syscall.Kill(-pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		c.clearReap(pid)
		return nil
	}
	if err != nil {
		return err
	}
	if c.reaps == nil {
		c.reaps = make(map[int]groupReap)
	}
	state := c.reaps[pid]
	now := time.Now()
	if state.term.IsZero() {
		state.term = now
		c.reaps[pid] = state
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return c.awaitLauncherGroup(pid)
	}
	if now.Sub(state.term) < StopTermGrace {
		return fmt.Errorf("process group %d still exists", pid)
	}
	if state.kill.IsZero() {
		state.kill = now
		c.reaps[pid] = state
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if err := c.awaitLauncherGroup(pid); err != nil {
		if now.Sub(state.kill) < StopKillGrace {
			return fmt.Errorf("process group %d still exists", pid)
		}
		return err
	}
	return nil
}

func (c *ProcessController) awaitLauncherGroup(pid int) error {
	deadline := time.Now().Add(launcherGroupReapPoll)
	for {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			c.clearReap(pid)
			return nil
		}
		if err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("process group %d still exists", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// GroupRestartRequest is the startup admission for a replacement agent.
// A queued activation is installed before that agent accepts claims. Applying,
// recovering and blocked phases already have a maintenance reservation, which
// the new agent reloads, so this returns the zero request for those phases.
// A repository read error makes known false and the supervisor defers the
// group start instead of opening claims first.
func GroupRestartRequest(state State, readErr error) (Request, bool) {
	if readErr != nil {
		return Request{}, false
	}
	if state.Pending != nil && state.Pending.Phase == "queued" {
		return state.Pending.Request, true
	}
	return Request{}, true
}

// RecoverAcceptedExits restarts an accepted launcher when the runtime and agent
// are still alive. An accepted agent or runtime exit stops the remaining group,
// confirms every exited process group is gone, and starts the bundle again.
// A replacement that dies quickly backs off from one second to thirty seconds.
// pending is the durable queued activation, or the zero request when none is
// queued. queueKnown is false when that queue could not be read. The replacement
// stays in recovery until it is ready. New execution claims stay closed until
// then. A runtime that is already executing a session is ready and is not
// stopped. A later activation that replaces its processes ends that wait.
// Called with no controller lock held.
func (c *ProcessController) RecoverAcceptedExits(ctx context.Context, pending Request, queueKnown bool) error {
	c.mu.Lock()
	c.abandonReplacedRecoveryLocked()
	if c.pendingGroup == "" {
		if c.active == "" || !acceptedServiceFailed(c.children) {
			err := c.restartExitedLauncherLocked()
			c.mu.Unlock()
			return err
		}
		c.pendingGroup = c.active
	}
	if c.recoveryOwnedLocked() && !acceptedServiceFailed(c.children) {
		id := c.pendingGroup
		c.mu.Unlock()
		return c.observeGroupReady(ctx, id)
	}
	if err := c.reapExitedGroupsLocked(); err != nil {
		c.mu.Unlock()
		return err
	}
	if !queueKnown {
		c.mu.Unlock()
		return errors.New("software queue unavailable; service group restart deferred")
	}
	reason := c.groupFailureReasonLocked()
	lived := c.failedServiceLifeLocked()
	id := c.pendingGroup
	running := len(c.children) != 0
	if !c.groupRestartDueLocked() {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if reason != "" {
		log.Printf("restarting service group after %s", reason)
	} else {
		log.Printf("restarting service group for %s", id)
	}
	if running {
		if err := c.Stop(ctx); err != nil {
			return err
		}
	}
	return c.finishGroupStart(ctx, id, lived, pending)
}

func acceptedServiceFailed(children []*child) bool {
	for _, p := range children {
		if p.role != roleRuntime && p.role != roleAgent {
			continue
		}
		select {
		case <-p.done:
			return true
		default:
		}
	}
	return false
}

func (c *ProcessController) reapExitedGroupsLocked() error {
	for _, p := range c.children {
		select {
		case <-p.done:
		default:
			continue
		}
		if err := c.confirmLauncherGroup(p); err != nil {
			return err
		}
	}
	return nil
}

func (c *ProcessController) groupFailureReasonLocked() string {
	var parts []string
	for _, p := range c.children {
		if p.role != roleAgent && p.role != roleRuntime {
			continue
		}
		select {
		case <-p.done:
			parts = append(parts, fmt.Sprintf("%s %s exited: %v", p.role, filepath.Base(p.path), p.err))
		default:
		}
	}
	return strings.Join(parts, "; ")
}

func (c *ProcessController) failedServiceLifeLocked() time.Duration {
	var lived time.Duration
	seen := false
	for _, p := range c.children {
		if p.role != roleAgent && p.role != roleRuntime {
			continue
		}
		select {
		case <-p.done:
			life := time.Since(p.started)
			if !seen || life < lived {
				lived = life
				seen = true
			}
		default:
		}
	}
	if !seen {
		return 0
	}
	return lived
}

func (c *ProcessController) groupRestartDueLocked() bool {
	if c.groupRestart.IsZero() {
		return true
	}
	delay := c.groupDelay
	if delay < launcherRestartMin {
		delay = launcherRestartMin
	}
	return time.Since(c.groupRestart) >= delay
}

func (c *ProcessController) noteGroupRestartLocked(lived time.Duration) {
	c.groupRestart = time.Now()
	if lived >= launcherStableLife {
		c.groupDelay = launcherRestartMin
		return
	}
	if c.groupDelay < launcherRestartMin {
		c.groupDelay = launcherRestartMin
		return
	}
	c.groupDelay *= 2
	if c.groupDelay > launcherRestartMax {
		c.groupDelay = launcherRestartMax
	}
}

func (c *ProcessController) finishGroupStart(ctx context.Context, id string, lived time.Duration, pending Request) error {
	c.mu.Lock()
	c.noteGroupRestartLocked(lived)
	c.holdRecoveryClaims = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.holdRecoveryClaims = false
		c.mu.Unlock()
	}()
	err := groupRestartStart(c, ctx, id, pending)
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), StopGroupTimeout)
		stopErr := c.Stop(stopCtx)
		cancel()
		if stopErr != nil {
			return fmt.Errorf("service group restart: %w; cleanup: %v", err, stopErr)
		}
		return err
	}
	c.mu.Lock()
	c.armRecoveryLocked(id)
	c.mu.Unlock()
	return c.observeGroupReady(ctx, id)
}

func (c *ProcessController) readyStableDuration() time.Duration {
	if c.readyStable > 0 {
		return c.readyStable
	}
	return groupReadyStable
}

func (c *ProcessController) readyBudgetDuration() time.Duration {
	if c.readyBudget > 0 {
		return c.readyBudget
	}
	return groupReadyBudget
}

func (c *ProcessController) armRecoveryLocked(id string) {
	c.recoveryID = id
	c.groupReadyBy = time.Now().Add(c.readyBudgetDuration())
	c.groupReadySince = time.Time{}
	c.recoveryPIDs = c.recoveryPIDs[:0]
	for _, p := range c.children {
		if p.cmd != nil && p.cmd.Process != nil {
			c.recoveryPIDs = append(c.recoveryPIDs, p.cmd.Process.Pid)
		}
	}
}

func (c *ProcessController) acceptRecoveryLocked() {
	c.pendingGroup = ""
	c.recoveryID = ""
	c.groupReadyBy = time.Time{}
	c.groupReadySince = time.Time{}
	c.recoveryPIDs = nil
	c.clearLauncherReap()
}

func (c *ProcessController) recoveryOwnedLocked() bool {
	if c.recoveryID == "" || c.groupReadyBy.IsZero() || c.active != c.recoveryID || len(c.recoveryPIDs) == 0 || len(c.children) != len(c.recoveryPIDs) {
		return false
	}
	for i, p := range c.children {
		if p.cmd == nil || p.cmd.Process == nil || p.cmd.Process.Pid != c.recoveryPIDs[i] {
			return false
		}
	}
	return true
}

// abandonReplacedRecoveryLocked drops a readiness wait whose processes are no
// longer the replacement. A healthy group with different processes was started
// by a later activation, so the queued restart is finished. A dead group keeps
// pendingGroup and the normal restart path.
func (c *ProcessController) abandonReplacedRecoveryLocked() {
	if c.recoveryID == "" || len(c.recoveryPIDs) == 0 || c.recoveryOwnedLocked() {
		return
	}
	replaced := c.active != "" && !acceptedServiceFailed(c.children)
	if replaced && c.pendingGroup == c.recoveryID {
		c.pendingGroup = ""
	}
	c.recoveryID = ""
	c.groupReadyBy = time.Time{}
	c.groupReadySince = time.Time{}
	c.recoveryPIDs = nil
}

// nativeSessionActive reports a runtime status for a core that has finished
// loading. execution=development with state=starting is an in-progress load,
// not a session, and stays on the readiness budget.
func nativeSessionActive(status nativeStatus) bool {
	return status.OK && status.State == "running_development" && status.Execution == "development"
}

// confirmRecoveryClaims polls until the execution fence is closed or the start
// context ends. The first dial can fail while the agent is still binding its
// socket. Called with c.mu held.
func (c *ProcessController) confirmRecoveryClaims(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := closeRecoveryClaims(c, callCtx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// runtimeExecuting reports that this replacement's own runtime is in a session.
// An admission status for a different bundle is not this group. An admission
// status that cannot be read does not hide a session the runtime itself reports.
func (c *ProcessController) runtimeExecuting(ctx context.Context, id string) bool {
	c.mu.Lock()
	children := append([]*child(nil), c.children...)
	active := c.active
	c.mu.Unlock()
	if active != id || len(children) != 3 {
		return false
	}
	var runtime *child
	for _, p := range children {
		if p.role == roleRuntime {
			runtime = p
		}
		select {
		case <-p.done:
			return false
		default:
		}
		if p.cmd == nil || p.cmd.Process == nil {
			return false
		}
		actual, err := os.Stat("/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/exe")
		if err != nil {
			return false
		}
		expected, err := os.Stat(p.path)
		if err != nil || !os.SameFile(actual, expected) {
			return false
		}
	}
	if runtime == nil || runtime.cmd == nil || runtime.cmd.Process == nil {
		return false
	}
	if c.Admission != nil {
		status, err := c.Admission.Status(ctx)
		if err == nil && status.BundleID != id {
			return false
		}
	}
	identity, err := processStartTime(runtime.cmd.Process.Pid)
	if err != nil {
		return false
	}
	native, err := readRecoveryStatus(ctx, "/run/mister-runtime.sock", runtime.cmd.Process.Pid, identity, runtime.path)
	if err != nil {
		return false
	}
	return nativeSessionActive(native)
}

func (c *ProcessController) recoveryOwned(id string) bool {
	return c.recoveryOwnedLocked() && c.recoveryID == id && c.pendingGroup == id
}

// observeGroupReady samples readiness once. The supervisor tick calls it again
// until the stable window holds. New execution claims stay closed until that
// window accepts the group, so a game cannot start and make the idle sample
// fail. A runtime that is already in a finished session is accepted once the
// fence is confirmed. A confirmed session whose fence cannot be closed stays
// running with claims closed; the next tick retries the fence and does not
// stop the runtime. A process exit returns to the restart path. When the
// budget expires first and the group is neither idle-ready nor executing, the
// wedged group is stopped and the next start waits out the backoff.
func (c *ProcessController) observeGroupReady(ctx context.Context, id string) error {
	c.mu.Lock()
	owned := c.recoveryOwned(id)
	c.mu.Unlock()
	if !owned {
		return nil
	}
	claimCtx, claimCancel := context.WithTimeout(ctx, time.Second)
	fenceErr := closeRecoveryClaims(c, claimCtx)
	claimCancel()

	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	probeErr := groupReadyProbe(c, probeCtx, id)
	cancel()

	c.mu.Lock()
	owned = c.recoveryOwned(id)
	expired := owned && !time.Now().Before(c.groupReadyBy)
	c.mu.Unlock()
	if !owned {
		return nil
	}
	if errors.Is(probeErr, ErrProcessExited) {
		c.mu.Lock()
		c.recoveryID = ""
		c.groupReadyBy = time.Time{}
		c.groupReadySince = time.Time{}
		c.recoveryPIDs = nil
		c.mu.Unlock()
		return nil
	}
	// A lost or rejected /fence leaves claims open, so that sample is not
	// accepted. A nil error is the only confirmed close. A finished session
	// the runtime itself reports is kept: the fence is retried and the group
	// is not stopped. Opening claims later uses the same rule: an uncertain
	// or rejected clear does not accept the group.
	executing := errors.Is(probeErr, errRecoveryExecuting)
	if fenceErr != nil && executing {
		c.mu.Lock()
		c.groupReadySince = time.Time{}
		c.mu.Unlock()
		return nil
	}
	if fenceErr != nil {
		probeErr = fenceErr
	} else if probeErr == nil || executing {
		return c.acceptReadyRecovery(ctx, id, executing)
	}
	if !expired {
		c.mu.Lock()
		c.groupReadySince = time.Time{}
		c.mu.Unlock()
		return nil
	}
	return c.stopUnreadyRecovery(probeErr)
}

func (c *ProcessController) stopUnreadyRecovery(probeErr error) error {
	stopCtx, stopCancel := context.WithTimeout(context.Background(), StopGroupTimeout)
	stopErr := c.Stop(stopCtx)
	stopCancel()
	c.mu.Lock()
	c.recoveryID = ""
	c.groupReadyBy = time.Time{}
	c.groupReadySince = time.Time{}
	c.recoveryPIDs = nil
	if c.groupRestartDueLocked() {
		c.noteGroupRestartLocked(0)
	}
	c.mu.Unlock()
	if stopErr != nil {
		return fmt.Errorf("service group not ready: %v; cleanup: %w", probeErr, stopErr)
	}
	return fmt.Errorf("service group not ready: %w", probeErr)
}

// acceptReadyRecovery records an idle streak and accepts the group once that
// streak holds, or immediately when the runtime is already executing. Claims
// are opened before acceptance. A failed open leaves the group owned so the
// next sample retries; it does not stop a ready or busy group.
func (c *ProcessController) acceptReadyRecovery(ctx context.Context, id string, executing bool) error {
	c.mu.Lock()
	if !c.recoveryOwned(id) {
		c.mu.Unlock()
		return nil
	}
	if !executing {
		if c.groupReadySince.IsZero() {
			c.groupReadySince = time.Now()
		}
		if time.Since(c.groupReadySince) < c.readyStableDuration() {
			c.mu.Unlock()
			return nil
		}
	}
	c.mu.Unlock()
	openCtx, openCancel := context.WithTimeout(ctx, time.Second)
	err := openRecoveryClaims(c, openCtx)
	openCancel()
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.recoveryOwned(id) {
		return nil
	}
	if executing {
		log.Printf("accepting recovered service group %s during runtime execution", id)
	}
	c.acceptRecoveryLocked()
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

// CheckNoManagedProcesses is a read-only confirmation that no supervised process
// runs: after the supervisor exits, and before factory selection. A failed stop
// cannot be disguised by deleting its PID file.
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
				return fmt.Errorf("managed software process %d is still running", pid)
			}
		}
	}
	return nil
}
