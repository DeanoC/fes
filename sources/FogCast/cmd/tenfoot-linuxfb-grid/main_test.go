package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"-nope"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
}

func TestResolveInputPathsNone(t *testing.T) {
	if got := resolveInputPaths("none"); len(got) != 0 {
		t.Fatalf("none: %v", got)
	}
	if got := resolveInputPaths(""); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
	got := resolveInputPaths("/dev/input/js0,/dev/input/js0")
	if len(got) != 1 || got[0] != "/dev/input/js0" {
		t.Fatalf("dedup %v", got)
	}
}

func TestRunOpenFailsForMissingFramebuffer(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-fb", filepath.Join(t.TempDir(), "missing-fb"), "-hold", "0"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code %d stdout %s stderr %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "linuxfb") {
		t.Fatalf("stderr %s", stderr.String())
	}
}
