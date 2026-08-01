package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDoesNotExposeMalformedConfigurationContents(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := "token = \"real-secret-token\"\nthis is not valid TOML\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), path, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("malformed configuration was accepted")
	}
	if strings.Contains(err.Error(), "real-secret-token") {
		t.Fatalf("configuration content leaked in error: %v", err)
	}
}
