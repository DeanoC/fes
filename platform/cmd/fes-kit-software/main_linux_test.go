package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/internal/kitsoftware"
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
	assertDevNullSession(t, cmd.Process.Pid)
}

func TestStartWithClosedPipeKeepsSupervisorAlive(t *testing.T) {
	switch os.Getenv("FES_KIT_SOFTWARE_PIPE_ROLE") {
	case "starter":
		startDetachedPipeSupervisor()
		return
	case "supervisor":
		serveSupervisorLog()
		return
	}

	dir := t.TempDir()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartWithClosedPipeKeepsSupervisorAlive$")
	cmd.Env = append(os.Environ(), "FES_KIT_SOFTWARE_PIPE_ROLE=starter", "FES_KIT_SOFTWARE_PIPE_DIR="+dir)
	cmd.Stdin = writer
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, reader) }()

	var supervisor *os.Process
	defer func() {
		if supervisor != nil {
			_ = supervisor.Kill()
		}
	}()
	starterDone := make(chan error, 1)
	go func() { starterDone <- cmd.Wait() }()

	pidText := waitForFile(t, filepath.Join(dir, "supervisor.pid"))
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil || pid <= 1 {
		t.Fatalf("supervisor pid %q", pidText)
	}
	supervisor, err = os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-starterDone:
		if err != nil {
			t.Fatalf("start exited: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("start did not return")
	}
	// The caller's SSH pipe is gone. The next supervisor log must not die with it.
	_ = reader.Close()
	if err := os.WriteFile(filepath.Join(dir, "write"), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(dir, "done"))
	if err := unix.Kill(pid, 0); err != nil {
		t.Fatalf("supervisor died after the caller pipe closed: %v", err)
	}
	assertDevNullSession(t, pid)
	assertSupervisorLogged(t, dir)
}

func TestBrokenStderrDoesNotKillSupervisor(t *testing.T) {
	if os.Getenv("FES_KIT_SOFTWARE_PIPE_ROLE") == "mirror" {
		serveSupervisorLog()
		return
	}
	dir := t.TempDir()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrokenStderrDoesNotKillSupervisor$")
	cmd.Env = append(os.Environ(), "FES_KIT_SOFTWARE_PIPE_ROLE=mirror", "FES_KIT_SOFTWARE_PIPE_DIR="+dir)
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	waitForFile(t, filepath.Join(dir, "ready"))
	_ = reader.Close()
	_ = writer.Close()
	if err := os.WriteFile(filepath.Join(dir, "write"), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(dir, "done"))
	if err := unix.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("supervisor died after stderr closed: %v", err)
	}
	assertSupervisorLogged(t, dir)
}

func startDetachedPipeSupervisor() {
	dir := os.Getenv("FES_KIT_SOFTWARE_PIPE_DIR")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartWithClosedPipeKeepsSupervisorAlive$")
	cmd.Env = append(os.Environ(), "FES_KIT_SOFTWARE_PIPE_ROLE=supervisor", "FES_KIT_SOFTWARE_PIPE_DIR="+dir)
	null, err := detachDaemon(cmd)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	if err := cmd.Start(); err != nil {
		_ = null.Close()
		log.Print(err)
		os.Exit(1)
	}
	if err := null.Close(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(dir, "supervisor.pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0600); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func serveSupervisorLog() {
	ignoreClosedPipe()
	dir := os.Getenv("FES_KIT_SOFTWARE_PIPE_DIR")
	logs, err := kitsoftware.OpenSupervisorLog(filepath.Join(dir, kitsoftware.SupervisorLogName), os.Stderr)
	if err != nil {
		os.Exit(1)
	}
	defer logs.Close()
	log.SetOutput(logs)
	if err := os.WriteFile(filepath.Join(dir, "ready"), []byte("1\n"), 0600); err != nil {
		os.Exit(1)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "write")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(1)
		}
		time.Sleep(5 * time.Millisecond)
	}
	log.Print("after the caller pipe closed")
	_, _ = os.Stderr.Write([]byte("stderr after close\n"))
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "done"), []byte("1\n"), 0600); err != nil {
		os.Exit(1)
	}
	select {}
}

func assertDevNullSession(t *testing.T, pid int) {
	t.Helper()
	for _, fd := range []string{"0", "1", "2"} {
		target, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/fd/" + fd)
		if err != nil {
			t.Fatal(err)
		}
		if target != "/dev/null" {
			t.Fatalf("fd %s -> %s", fd, target)
		}
	}
	sid, err := unix.Getsid(pid)
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := unix.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != pid || pgid != pid {
		t.Fatalf("session %d process group %d pid %d", sid, pgid, pid)
	}
}

func assertSupervisorLogged(t *testing.T, dir string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, kitsoftware.SupervisorLogName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "after the caller pipe closed") {
		t.Fatalf("supervisor log = %q", body)
	}
}

func waitForFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(body))) > 0 {
			return body
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}
