package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedLinkPreservesOutput(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{}
	for name, data := range map[string]string{"base": "invalid", "map": "{}", "rom": "short", "output": "keep"} {
		paths[name] = filepath.Join(dir, name)
		if err := os.WriteFile(paths[name], []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := run(context.Background(), []string{"-base", paths["base"], "-map", paths["map"], "-rom", paths["rom"], "-output", paths["output"]})
	if err == nil {
		t.Fatal("invalid link succeeded")
	}
	got, err := os.ReadFile(paths["output"])
	if err != nil || string(got) != "keep" {
		t.Fatalf("failed link replaced output: %q %v", got, err)
	}
}
