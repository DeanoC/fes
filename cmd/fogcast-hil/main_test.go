package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hil"
)

func TestCLIRequiresExplicitGameIDs(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(nil, []string{"--config", filepath.Join(t.TempDir(), "missing.toml")}, strings.NewReader(""), &out, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want usage 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--sonic-id") || !strings.Contains(stderr.String(), "--mario-id") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIValidatesIDsBeforeLoadingConfiguration(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(nil, []string{"--config", filepath.Join(t.TempDir(), "missing.toml"), "--sonic-id", "sonic-test", "--mario-id", "mario-test"}, strings.NewReader(""), &out, &stderr)
	if code == 2 {
		t.Fatalf("explicit IDs unexpectedly rejected as usage: %q", stderr.String())
	}
}

func TestWritePOC2ReportIsPrivateAndRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	report := hil.Report{StartedAt: time.Unix(1, 0).UTC(), FinishedAt: time.Unix(2, 0).UTC(), Checks: []hil.Check{{Name: "scan", Passed: true, Detail: "ok"}}, Passed: true}
	path := filepath.Join(dir, "nested", "report.json")
	if err := writeReport(path, report); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	outside := filepath.Join(dir, "outside.json")
	if err := os.WriteFile(outside, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := writeReport(link, report); err == nil {
		t.Fatal("symlink report path was overwritten")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sentinel" {
		t.Fatalf("symlink target changed: %q", data)
	}
}

func TestWritePOC2ReportRejectsPrivateValues(t *testing.T) {
	report := hil.Report{StartedAt: time.Unix(1, 0).UTC(), FinishedAt: time.Unix(2, 0).UTC(), Checks: []hil.Check{{Name: "audit", Passed: false, Detail: "Bearer private-token at /media/fat/fogcast/cache/game.sfc"}}}
	if err := writeReport(filepath.Join(t.TempDir(), "report.json"), report); err == nil {
		t.Fatal("report writer accepted private values")
	}
}
