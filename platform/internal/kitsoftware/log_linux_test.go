package kitsoftware

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisorLogRotatesAndMirrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SupervisorLogName)
	var mirror lockedBuffer
	logFile, err := newSupervisorLog(path, 64, &mirror)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	if _, err := logFile.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := logFile.Write([]byte(second)); err != nil {
		t.Fatal(err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != second {
		t.Fatalf("current log = %q", current)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(previous) != first {
		t.Fatalf("previous log = %q", previous)
	}
	waitForMirror(t, &mirror, first+second)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestSupervisorLogRejectsClosedWrite(t *testing.T) {
	logFile, err := newSupervisorLog(filepath.Join(t.TempDir(), SupervisorLogName), 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := logFile.Write([]byte("x")); err == nil {
		t.Fatal("closed log accepted a write")
	}
}

func TestSupervisorLogWriteDoesNotWaitForConsole(t *testing.T) {
	path := filepath.Join(t.TempDir(), SupervisorLogName)
	mirror := &gateWriter{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(mirror.release) }) }
	defer release()
	logFile, err := newSupervisorLog(path, 1<<20, mirror)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("hello-log")
	if err := writeWithin(t, logFile, original); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mirror.entered:
	case <-time.After(time.Second):
		t.Fatal("console mirror did not receive the record")
	}
	copy(original, "GOODBYE!!")
	overflow := bytes.Repeat([]byte("z"), supervisorMirrorQueueBytes)
	if err := writeWithin(t, logFile, overflow); err != nil {
		t.Fatal(err)
	}
	release()
	waitForMirror(t, mirror, "hello-log")
	if strings.Contains(mirror.String(), "GOODBYE") {
		t.Fatal("console mirror observed a buffer the caller reused")
	}
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte("hello-log")) || !bytes.Contains(body, overflow) {
		t.Fatalf("file log lost bytes while the console was stalled: %d bytes", len(body))
	}
}

func TestSupervisorLogRetriesOpenAfterFailedRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), SupervisorLogName)
	logFile, err := newSupervisorLog(path, 32, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logFile.Write([]byte(strings.Repeat("a", 20))); err != nil {
		t.Fatal(err)
	}
	var opens int
	logFile.openLog = func(name string) (*os.File, error) {
		opens++
		if opens <= 2 {
			return nil, errors.New("temporary inode exhaustion")
		}
		return os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	}
	if _, err := logFile.Write([]byte(strings.Repeat("b", 20))); err == nil {
		t.Fatal("rotation open failure was ignored")
	}
	if logFile.closed {
		t.Fatal("transient rotation failure closed the log")
	}
	recovered := []byte("recovered")
	if _, err := logFile.Write(recovered); err != nil {
		t.Fatal(err)
	}
	logFile.openLog = func(string) (*os.File, error) {
		t.Error("closed log tried to open again")
		return nil, errors.New("closed")
	}
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := logFile.Write([]byte("x")); err == nil {
		t.Fatal("closed log accepted a write")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(recovered) {
		t.Fatalf("current log = %q", current)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(previous) != strings.Repeat("a", 20) {
		t.Fatalf("previous log = %q", previous)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

type gateWriter struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (g *gateWriter) Write(p []byte) (int, error) {
	g.once.Do(func() {
		close(g.entered)
		<-g.release
	})
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Write(p)
}

func (g *gateWriter) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

func writeWithin(t *testing.T, logFile *SupervisorLog, p []byte) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		_, err := logFile.Write(p)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(time.Second):
		t.Fatal("log write blocked on the console mirror")
	}
	return nil
}

func waitForMirror(t *testing.T, mirror interface{ String() string }, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = mirror.String()
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mirror = %q, want %q", got, want)
}
