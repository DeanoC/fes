package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgsRequiresExactlyOneValueForEveryFlag(t *testing.T) {
	base := []string{"--lock", "lock.toml", "--source", "/src", "--toolchain-archive", "/toolchain.tar.xz", "--left", "/left", "--right", "/right", "--report", "/report.json"}
	if _, ok := parseArgs(base); !ok {
		t.Fatal("parseArgs(valid) rejected complete request")
	}
	for _, hostile := range [][]string{
		{},
		append([]string(nil), base[:len(base)-1]...),
		append(append([]string(nil), base...), "--left", "/other"),
		append(append([]string(nil), base...), "--unknown", "value"),
	} {
		if _, ok := parseArgs(hostile); ok {
			t.Fatalf("parseArgs(%q) accepted malformed request", hostile)
		}
	}
}

func TestRunInvalidLockFailsClosedWithoutReport(t *testing.T) {
	base := t.TempDir()
	lockPath := filepath.Join(base, "lock.toml")
	if err := os.WriteFile(lockPath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"--lock", lockPath,
		"--source", filepath.Join(base, "source"),
		"--toolchain-archive", filepath.Join(base, "toolchain.tar.xz"),
		"--left", filepath.Join(base, "left"),
		"--right", filepath.Join(base, "right"),
		"--report", filepath.Join(base, "report.json"),
	}
	var stdout, stderr bytes.Buffer
	if got := run(args, &stdout, &stderr); got != exitGate {
		t.Fatalf("run(invalid lock) = %d, want %d; stderr=%q", got, exitGate, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("invalid lock wrote stdout: %q", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(base, "report.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid lock created report: %v", err)
	}
}
