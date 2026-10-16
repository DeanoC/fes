package kitsoftware

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorLogRotatesAndMirrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SupervisorLogName)
	var mirror bytes.Buffer
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
	if mirror.String() != first+second {
		t.Fatalf("mirror = %q", mirror.String())
	}
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
