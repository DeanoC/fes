package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hil"
)

func TestTerminalPrompterConfirmsOnlyExplicitYes(t *testing.T) {
	t.Parallel()
	input := strings.NewReader("yes\nn\n")
	var output bytes.Buffer
	prompt := newTerminalPrompter(input, &output)
	confirmed, err := prompt.Confirm("first check")
	if err != nil || !confirmed {
		t.Fatalf("first confirmation = %v, %v", confirmed, err)
	}
	confirmed, err = prompt.Confirm("second check")
	if err != nil || confirmed {
		t.Fatalf("second confirmation = %v, %v", confirmed, err)
	}
	if got := output.String(); got != "first check [y/N]: second check [y/N]: " {
		t.Fatalf("prompt output = %q", got)
	}
}

func TestWriteReportIsIndentedAndPrivate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	report := hil.Report{
		StartedAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 1, 0, 0, 1, 0, time.UTC),
		Checks:     []hil.Check{{Name: "test", Passed: true, Detail: "ok"}},
		Passed:     true,
	}
	if err := writeReport(path, report); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("report mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("\n  \"started_at\"")) || !json.Valid(data) {
		t.Fatalf("report is not indented JSON: %s", data)
	}
}
