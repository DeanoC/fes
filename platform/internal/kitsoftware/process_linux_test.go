package kitsoftware

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTermIgnoringProcessFixture(t *testing.T) {
	ready := os.Getenv("FES_TEST_IGNORE_TERM_READY")
	if ready == "" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestProcessStopKillsAndReapsTermIgnoringChild(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	controller := &ProcessController{RunDir: dir}
	if err := controller.start(binary, []string{"-test.run=^TestTermIgnoringProcessFixture$"}, []string{"FES_TEST_IGNORE_TERM_READY=" + ready}, ""); err != nil {
		t.Fatal(err)
	}
	child := controller.children[0]
	defer func() { _ = child.cmd.Process.Kill(); <-child.done }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("signal fixture did not initialize")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	if err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < StopTermGrace {
		t.Fatal("TERM-ignoring process reported exit before escalation")
	}
	select {
	case <-child.done:
	default:
		t.Fatal("replacement permitted before old process reaped")
	}
	if len(controller.children) != 0 {
		t.Fatal("stopped group retained")
	}
}
func TestProcessStopBlocksWhenGroupMemberSurvivesParent(t *testing.T) {
	controller := &ProcessController{RunDir: t.TempDir()}
	if err := controller.start("/bin/sleep", []string{"60"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	parent := controller.children[0]
	// A child normally inherits its parent's group. Creating another process in
	// that exact group lets this test reap it itself instead of leaking a zombie
	// to the test runner's PID 1 when the group leader exits.
	member := exec.Command("/bin/sleep", "60")
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: parent.cmd.Process.Pid}
	if err := member.Start(); err != nil {
		_ = parent.cmd.Process.Kill()
		<-parent.done
		t.Fatal(err)
	}
	defer func() { _ = member.Process.Kill(); _ = member.Wait() }()
	if err := parent.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-parent.done
	if err := controller.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatal("surviving group member did not block replacement", err)
	}
	if len(controller.children) != 1 {
		t.Fatal("failed stop discarded process ownership")
	}
}

func TestAgentConfigWaitAppearsAndExpires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.toml")
	ctx := context.Background()
	started := time.Now()
	go func() {
		time.Sleep(40 * time.Millisecond)
		if err := os.WriteFile(path, []byte("target"), 0600); err != nil {
			t.Error(err)
		}
	}()
	if err := waitForAgentConfig(ctx, path, time.Second); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) >= time.Second {
		t.Fatal("waited for the full configuration deadline")
	}
	if err := waitForAgentConfig(ctx, filepath.Join(dir, "missing.toml"), 30*time.Millisecond); err == nil {
		t.Fatal("missing configuration succeeded")
	}
}

func TestLauncherProbeTimeoutDoesNotFollowParentContext(t *testing.T) {
	dir := t.TempDir()
	kit := filepath.Join(dir, "fogcast-kit")
	// Sleep in this process. A shell waiting on a child would keep the output
	// pipe open after the shell itself was killed.
	if err := os.WriteFile(kit, []byte("#!/usr/bin/python3\nimport time\ntime.sleep(30)\n"), 0700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := probeLauncherUI(context.Background(), kit, 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("probe followed an unbounded parent context", time.Since(started))
	}
}

func TestRestartExitedLauncherRestartsOnlyTheLauncher(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer func() { _ = c.Stop(context.Background()) }()
	for _, role := range []string{roleRuntime, roleAgent} {
		if err := c.start("/bin/sleep", []string{"60"}, nil, role); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.start("/bin/true", nil, nil, roleLauncher); err != nil {
		t.Fatal(err)
	}
	launcher := c.children[2]
	<-launcher.done
	if !strings.Contains(c.ServiceFault(), "restarting the launcher") {
		t.Fatal(c.ServiceFault())
	}
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.children[2] == launcher {
		t.Fatal("launcher was not replaced")
	}
	replaced := c.children[2]
	<-replaced.done
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.children[2] != replaced {
		t.Fatal("launcher restarted again inside the backoff window")
	}
	runtimePID := c.children[0].cmd.Process.Pid
	agentPID := c.children[1].cmd.Process.Pid
	if err := c.children[0].cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-c.children[0].done
	if !strings.Contains(c.ServiceFault(), "restarting the service group") {
		t.Fatal(c.ServiceFault())
	}
	starts := installGroupRestart(t, c)
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts() != 1 || len(c.children) != 3 {
		t.Fatal("runtime exit did not restart the service group", starts(), len(c.children))
	}
	if c.children[0].cmd.Process.Pid == runtimePID || c.children[1].cmd.Process.Pid == agentPID || c.Active() != "accepted" {
		t.Fatal("group restart kept a process from the dead runtime's group", c.Active())
	}
	for _, pid := range []int{runtimePID, agentPID, replaced.cmd.Process.Pid} {
		assertProcessGone(t, pid)
	}
}

func TestRestartExitedLauncherReapsDescendantsBeforeReplacement(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer killChildren(c)
	launcher := startSleepTrio(t, c)
	member := exec.Command("/bin/sleep", "60")
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: launcher.cmd.Process.Pid}
	if err := member.Start(); err != nil {
		t.Fatal(err)
	}
	memberDone := make(chan struct{})
	go func() {
		_ = member.Wait()
		close(memberDone)
	}()
	defer func() {
		_ = member.Process.Kill()
		<-memberDone
	}()
	if err := launcher.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-launcher.done
	if err := member.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("descendant exited with the launcher", err)
	}
	oldPID := launcher.cmd.Process.Pid
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.children) != 3 || c.children[2] == launcher || c.children[2].cmd.Process.Pid == oldPID {
		t.Fatal("replacement did not start after the old launcher group exited")
	}
	select {
	case <-memberDone:
	case <-time.After(time.Second):
		t.Fatal("descendant survived launcher replacement")
	}
}

func TestRestartExitedLauncherKeepsOwnershipUntilGroupExits(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &ProcessController{RunDir: dir, active: "accepted"}
	defer killChildren(c)
	launcher := startSleepTrio(t, c)
	member := exec.Command(binary, "-test.run=^TestTermIgnoringProcessFixture$")
	member.Env = append(os.Environ(), "FES_TEST_IGNORE_TERM_READY="+ready)
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: launcher.cmd.Process.Pid}
	if err := member.Start(); err != nil {
		t.Fatal(err)
	}
	memberDone := make(chan struct{})
	go func() {
		_ = member.Wait()
		close(memberDone)
	}()
	defer func() {
		_ = member.Process.Kill()
		<-memberDone
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("signal fixture did not initialize")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := launcher.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-launcher.done
	if err := c.RecoverAcceptedExits(context.Background()); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatal("replacement started while a descendant was still in the launcher group", err)
	}
	if len(c.children) != 3 || c.children[2] != launcher {
		t.Fatal("cleanup failure dropped launcher ownership")
	}
	select {
	case <-memberDone:
		t.Fatal("descendant exited before the group was confirmed gone")
	default:
	}
	if err := c.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatal("activation stop ignored the surviving launcher group", err)
	}
	if len(c.children) != 3 || c.children[2] != launcher {
		t.Fatal("failed stop cleared ownership while the launcher group existed")
	}
	if c.reapTerm.IsZero() {
		t.Fatal("group cleanup did not record a retry deadline")
	}
	time.Sleep(time.Until(c.reapTerm.Add(StopTermGrace)) + 20*time.Millisecond)
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.children[2] == launcher {
		t.Fatal("launcher was not replaced after its process group was killed")
	}
	select {
	case <-memberDone:
	case <-time.After(time.Second):
		t.Fatal("descendant survived group kill")
	}
}

func TestAcceptedAgentOrRuntimeExitRestartsTheGroup(t *testing.T) {
	for _, index := range []int{1, 0} {
		role := []string{roleRuntime, roleAgent}[index]
		t.Run(role, func(t *testing.T) {
			c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
			defer killChildren(c)
			_ = startSleepTrio(t, c)
			starts := installGroupRestart(t, c)
			if err := c.RecoverAcceptedExits(context.Background()); err != nil {
				t.Fatal(err)
			}
			if starts() != 0 {
				t.Fatal("healthy group restarted")
			}
			dead := c.children[index]
			old := []int{c.children[0].cmd.Process.Pid, c.children[1].cmd.Process.Pid, c.children[2].cmd.Process.Pid}
			if err := dead.cmd.Process.Signal(syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			<-dead.done
			if !strings.Contains(c.ServiceFault(), "restarting the service group") {
				t.Fatal(c.ServiceFault())
			}
			if err := c.RecoverAcceptedExits(context.Background()); err != nil {
				t.Fatal(err)
			}
			if starts() != 1 || len(c.children) != 3 || c.Active() != "accepted" || c.pendingGroup != "" {
				t.Fatal("service exit did not restart the group", starts(), len(c.children), c.Active(), c.pendingGroup)
			}
			for i, pid := range old {
				if c.children[i].cmd.Process.Pid == pid {
					t.Fatalf("role %d kept pid %d", i, pid)
				}
				assertProcessGone(t, pid)
			}
			if err := c.children[index].cmd.Process.Signal(syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			<-c.children[index].done
			if err := c.RecoverAcceptedExits(context.Background()); err != nil {
				t.Fatal(err)
			}
			if starts() != 1 {
				t.Fatal("group restarted again inside the backoff window", starts())
			}
			if !strings.Contains(c.ServiceFault(), "restarting the service group") {
				t.Fatal(c.ServiceFault())
			}
		})
	}
}

func TestAgentExitReapsDescendantsBeforeGroupRestart(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer killChildren(c)
	_ = startSleepTrio(t, c)
	agent := c.children[1]
	member := exec.Command("/bin/sleep", "60")
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: agent.cmd.Process.Pid}
	if err := member.Start(); err != nil {
		t.Fatal(err)
	}
	memberDone := make(chan struct{})
	go func() {
		_ = member.Wait()
		close(memberDone)
	}()
	defer func() {
		_ = member.Process.Kill()
		<-memberDone
	}()
	memberPID := member.Process.Pid
	var starts int
	previous := groupRestartStart
	t.Cleanup(func() { groupRestartStart = previous })
	groupRestartStart = func(controller *ProcessController, _ context.Context, id string, request Request) error {
		assertProcessGone(t, memberPID)
		starts++
		return replaceAcceptedGroup(controller, id, request)
	}
	if err := agent.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	<-agent.done
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || len(c.children) != 3 {
		t.Fatal("agent descendants did not clear before the replacement", starts, len(c.children))
	}
	select {
	case <-memberDone:
	case <-time.After(time.Second):
		t.Fatal("agent descendant survived group restart")
	}
}

func TestAgentExitKeepsOwnershipUntilDescendantsExit(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &ProcessController{RunDir: dir, active: "accepted"}
	defer killChildren(c)
	_ = startSleepTrio(t, c)
	agent := c.children[1]
	member := exec.Command(binary, "-test.run=^TestTermIgnoringProcessFixture$")
	member.Env = append(os.Environ(), "FES_TEST_IGNORE_TERM_READY="+ready)
	member.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: agent.cmd.Process.Pid}
	if err := member.Start(); err != nil {
		t.Fatal(err)
	}
	memberDone := make(chan struct{})
	go func() {
		_ = member.Wait()
		close(memberDone)
	}()
	defer func() {
		_ = member.Process.Kill()
		<-memberDone
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("signal fixture did not initialize")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := agent.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	<-agent.done
	starts := installGroupRestart(t, c)
	if err := c.RecoverAcceptedExits(context.Background()); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatal("replacement started while an agent descendant was still in the group", err)
	}
	if starts() != 0 || len(c.children) != 3 || c.children[1] != agent || c.pendingGroup != "accepted" {
		t.Fatal("cleanup failure dropped the service group", starts(), len(c.children), c.pendingGroup)
	}
	select {
	case <-memberDone:
		t.Fatal("descendant exited before the group was confirmed gone")
	default:
	}
	if c.reapTerm.IsZero() {
		t.Fatal("group cleanup did not record a retry deadline")
	}
	time.Sleep(time.Until(c.reapTerm.Add(StopTermGrace)) + 20*time.Millisecond)
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts() != 1 || c.children[1] == agent {
		t.Fatal("group was not replaced after the agent process group was killed", starts())
	}
	select {
	case <-memberDone:
	case <-time.After(time.Second):
		t.Fatal("descendant survived group kill")
	}
}

func TestGroupRestartFailureDoesNotLeaveProcesses(t *testing.T) {
	c := &ProcessController{RunDir: t.TempDir(), active: "accepted"}
	defer killChildren(c)
	_ = startSleepTrio(t, c)
	agent := c.children[1]
	if err := agent.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	<-agent.done
	var starts int
	previous := groupRestartStart
	t.Cleanup(func() { groupRestartStart = previous })
	groupRestartStart = func(controller *ProcessController, _ context.Context, id string, request Request) error {
		starts++
		if id != "accepted" || request.ID != "" {
			t.Fatalf("restart id %s request %+v", id, request)
		}
		if len(controller.children) != 0 {
			t.Fatal("partial restart began while old processes were owned")
		}
		if err := controller.start("/bin/sleep", []string{"60"}, nil, roleRuntime); err != nil {
			return err
		}
		return errors.New("native initialization timed out")
	}
	err := c.RecoverAcceptedExits(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatal(err)
	}
	if len(c.children) != 0 || c.pendingGroup != "accepted" || c.Active() != "" {
		t.Fatal("failed restart left a process or dropped the bundle", len(c.children), c.pendingGroup, c.Active())
	}
	if err := c.RecoverAcceptedExits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatal("failed restart ignored backoff", starts)
	}
}

func TestGroupRestartBackoffCapsAndResets(t *testing.T) {
	c := &ProcessController{}
	if !c.groupRestartDueLocked() {
		t.Fatal("first group restart waited")
	}
	c.noteGroupRestartLocked(0)
	if c.groupDelay != time.Second || c.groupRestartDueLocked() {
		t.Fatal("short life did not arm a one second backoff", c.groupDelay, c.groupRestartDueLocked())
	}
	c.groupRestart = time.Now().Add(-time.Second)
	if !c.groupRestartDueLocked() {
		t.Fatal("backoff did not expire")
	}
	c.noteGroupRestartLocked(0)
	if c.groupDelay != 2*time.Second {
		t.Fatal(c.groupDelay)
	}
	c.noteGroupRestartLocked(launcherStableLife)
	if c.groupDelay != time.Second {
		t.Fatal("stable life did not reset backoff", c.groupDelay)
	}
	c.groupDelay = 20 * time.Second
	c.noteGroupRestartLocked(0)
	if c.groupDelay != 30*time.Second {
		t.Fatal(c.groupDelay)
	}
	c.noteGroupRestartLocked(0)
	if c.groupDelay != 30*time.Second {
		t.Fatal("backoff exceeded the cap", c.groupDelay)
	}
}

func startSleepTrio(t *testing.T, c *ProcessController) *child {
	t.Helper()
	for _, role := range []string{roleRuntime, roleAgent} {
		if err := c.start("/bin/sleep", []string{"60"}, nil, role); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.start("/bin/sleep", []string{"60"}, nil, roleLauncher); err != nil {
		t.Fatal(err)
	}
	return c.children[2]
}

func installGroupRestart(t *testing.T, c *ProcessController) func() int {
	t.Helper()
	var starts int
	previous := groupRestartStart
	groupRestartStart = func(controller *ProcessController, _ context.Context, id string, request Request) error {
		starts++
		return replaceAcceptedGroup(controller, id, request)
	}
	t.Cleanup(func() { groupRestartStart = previous })
	return func() int { return starts }
}

func replaceAcceptedGroup(c *ProcessController, id string, request Request) error {
	if id != "accepted" {
		return errors.New("restart bundle " + id)
	}
	if request.ID != "" || request.Requester != "" {
		return errors.New("group restart replayed admission")
	}
	if len(c.children) != 0 {
		return errors.New("group restart left old processes owned")
	}
	for _, role := range []string{roleRuntime, roleAgent, roleLauncher} {
		if err := c.start("/bin/sleep", []string{"60"}, nil, role); err != nil {
			return err
		}
	}
	c.active = id
	return nil
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return
	}
	t.Fatalf("process %d still exists: %v", pid, err)
}

func killChildren(c *ProcessController) {
	for _, child := range c.children {
		if child.cmd.Process != nil {
			_ = child.cmd.Process.Kill()
		}
		select {
		case <-child.done:
		case <-time.After(2 * time.Second):
		}
	}
}
