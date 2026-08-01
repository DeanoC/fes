package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRequiresAllArguments(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code == 0 {
		t.Fatal("missing arguments succeeded")
	}
}

func TestRunRejectsInvalidTime(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if code := run([]string{"--source", "in", "--output", "out", "--time", "today"}, &stderr); code == 0 {
		t.Fatal("invalid RFC3339 time succeeded")
	}
}

func TestRunCreatesArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"MiSTer.ini.fragment": "[MiSTer]\n",
		"agent.toml":          "token",
		"mister-agent":        "binary",
		"start-agent.sh":      "#!/bin/sh\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "output.tar.gz")
	var stderr bytes.Buffer
	if code := run([]string{"--source", source, "--output", output, "--time", "2026-08-01T00:00:00Z"}, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("archive stat = %v, %v", info, err)
	}
}
