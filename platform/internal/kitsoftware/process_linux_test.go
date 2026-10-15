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
	if err := controller.start(binary, []string{"-test.run=^TestTermIgnoringProcessFixture$"}, []string{"FES_TEST_IGNORE_TERM_READY=" + ready}); err != nil {
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
	if err := controller.start("/bin/sleep", []string{"60"}, nil); err != nil {
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
