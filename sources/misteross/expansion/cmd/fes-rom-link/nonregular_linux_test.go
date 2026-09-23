package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := boundedRead(path, 1024); err == nil {
		t.Fatal("FIFO accepted")
	}
}
