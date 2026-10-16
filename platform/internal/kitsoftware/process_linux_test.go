package kitsoftware

import (
	"context"
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
	if err := c.RestartExitedLauncher(); err != nil {
		t.Fatal(err)
	}
	if c.children[2] == launcher {
		t.Fatal("launcher was not replaced")
	}
	replaced := c.children[2]
	<-replaced.done
	if err := c.RestartExitedLauncher(); err != nil {
		t.Fatal(err)
	}
	if c.children[2] != replaced {
		t.Fatal("launcher restarted again inside the backoff window")
	}
	if err := c.children[0].cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-c.children[0].done
	if err := c.RestartExitedLauncher(); err != nil {
		t.Fatal(err)
	}
	if c.children[2] != replaced {
		t.Fatal("launcher restart ran without a live runtime")
	}
	if !strings.Contains(c.ServiceFault(), "explicit supervisor restart") {
		t.Fatal(c.ServiceFault())
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
	if err := c.RestartExitedLauncher(); err != nil {
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
	if err := c.RestartExitedLauncher(); err == nil || !strings.Contains(err.Error(), "still exists") {
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
	if err := c.RestartExitedLauncher(); err != nil {
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
