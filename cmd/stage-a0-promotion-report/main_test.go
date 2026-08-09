package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/promotion"
)

func TestRunWritesBlockedCandidateReport(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "candidate.lock.toml")
	material := filepath.Join(root, "materials.json")
	comparison := filepath.Join(root, "comparison.json")
	policyDir := filepath.Join(root, "policies")
	output := filepath.Join(root, "report.json")
	for filename, raw := range map[string][]byte{
		lock:       []byte("format = 1\n"),
		material:   []byte("not-canonical-materials"),
		comparison: []byte("not-canonical-comparison"),
	} {
		if err := os.WriteFile(filename, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(policyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--lock", lock,
		"--policy-dir", policyDir,
		"--materials", material,
		"--comparison", comparison,
		"--output", output,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() code = %d, want blocked exit 1; stderr=%q", code, stderr.String())
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("report was not written: %v", err)
	}
	report, err := promotion.DecodeReport(raw)
	if err != nil {
		t.Fatalf("promotion report is invalid: %v", err)
	}
	if report.Decision != promotion.DecisionBlocked || !strings.Contains(stdout.String(), "blocked") {
		t.Fatalf("report/output = %#v/%q", report, stdout.String())
	}
}

func TestParseArgsRejectsDuplicateAndMissingFlags(t *testing.T) {
	if _, ok := parseArgs([]string{"--lock", "/a"}); ok {
		t.Fatal("parseArgs accepted incomplete arguments")
	}
	if _, ok := parseArgs([]string{
		"--lock", "/a", "--lock", "/b",
		"--policy-dir", "/p", "--materials", "/m", "--comparison", "/c", "--output", "/o",
	}); ok {
		t.Fatal("parseArgs accepted duplicate arguments")
	}
}
