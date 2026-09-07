package main

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"-nope"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
}

func TestRunOpenFailsOffKit(t *testing.T) {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		t.Skip("would open a real framebuffer")
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-fb", "/dev/fb0", "-hold", "0"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code %d stdout %s stderr %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "linuxfb") {
		t.Fatalf("stderr %s", stderr.String())
	}
}
