package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDetachDaemonUsesDevNullAndNewSession(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	null, err := detachDaemon(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = null.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	if err := null.Close(); err != nil {
		t.Fatal(err)
	}
	for _, fd := range []string{"0", "1", "2"} {
		target, err := os.Readlink("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/fd/" + fd)
		if err != nil {
			t.Fatal(err)
		}
		if target != "/dev/null" {
			t.Fatalf("fd %s -> %s", fd, target)
		}
	}
	sid, err := unix.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != cmd.Process.Pid {
		t.Fatalf("session %d, pid %d", sid, cmd.Process.Pid)
	}
}

func TestBrokenStderrDoesNotKillSupervisor(t *testing.T) {
	if os.Getenv("FES_KIT_SOFTWARE_SIGPIPE") == "1" {
		ignoreClosedPipe()
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			_, _ = os.Stderr.Write([]byte("supervisor mirror\n"))
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestBrokenStderrDoesNotKillSupervisor")
	cmd.Env = append(os.Environ(), "FES_KIT_SOFTWARE_SIGPIPE=1")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = writer
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	_ = reader.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("supervisor child did not exit after a closed stderr")
	}
}
