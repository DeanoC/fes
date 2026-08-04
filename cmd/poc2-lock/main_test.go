package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndVerifyCommandsRequireExactArguments(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		nil,
		{"record"},
		{"verify"},
		{"future"},
		{"record", "--poc1a-lock", "a", "--poc1b-lock", "b", "--prod", "p", "--dev", "d", "--output", "o", "extra"},
		{"verify", "--lock", "l", "--poc1a-lock", "a", "--poc1b-lock", "b", "--prod", "p", "--dev", "d", "extra"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) = %d, want usage failure; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestRecordAndVerifyCommandsUsePOC2Lock(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "outputs.poc2.lock.toml")
	record := []string{
		"record",
		"--poc1a-lock", filepath.Join(repo, "build", "sources.poc1a.lock.toml"),
		"--poc1b-lock", filepath.Join(repo, "build", "sources.poc1b.lock.toml"),
		"--prod", filepath.Join(repo, "build", "output", "poc1b", "prod", "linux.img"),
		"--dev", filepath.Join(repo, "build", "output", "poc1b", "dev", "linux.img"),
		"--output", output,
	}
	var stdout, stderr bytes.Buffer
	if code := run(record, &stdout, &stderr); code != 0 {
		t.Fatalf("record returned %d: %s", code, stderr.String())
	}
	if info, err := os.Stat(output); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("recorded output = %v, err = %v", info, err)
	}
	verify := []string{
		"verify",
		"--lock", output,
		"--poc1a-lock", filepath.Join(repo, "build", "sources.poc1a.lock.toml"),
		"--poc1b-lock", filepath.Join(repo, "build", "sources.poc1b.lock.toml"),
		"--prod", filepath.Join(repo, "build", "output", "poc1b", "prod", "linux.img"),
		"--dev", filepath.Join(repo, "build", "output", "poc1b", "dev", "linux.img"),
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(verify, &stdout, &stderr); code != 0 {
		t.Fatalf("verify returned %d: %s", code, stderr.String())
	}
	if stdout.String() != "POC 2 outputs verified\n" {
		t.Fatalf("verify stdout = %q", stdout.String())
	}
}
