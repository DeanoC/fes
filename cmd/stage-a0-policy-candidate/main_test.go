package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgsRequiresExactlyOneValueForEachFlag(t *testing.T) {
	valid := []string{
		"--repository", "/fork",
		"--source-material", "main-fork",
		"--authority", "/authority.json",
		"--build-log", "/build.log",
		"--receipt", "/receipt.json",
		"--output-dir", "/out",
	}
	req, ok := parseArgs(valid)
	if !ok || req.repository != "/fork" || req.outputDir != "/out" {
		t.Fatalf("parseArgs(valid) = %#v, %v", req, ok)
	}

	cases := [][]string{
		{},
		{"--repository"},
		{"--repository", "/fork", "--repository", "/other"},
		{"--repository", "/fork", "--source-material", "main-fork", "--authority", "/authority.json", "--build-log", "/build.log", "--receipt", "/receipt.json", "--output-dir", "/out", "--unknown", "x"},
		{"--repository", "/fork", "--source-material", "main-fork", "--authority", "/authority.json", "--build-log", "/build.log", "--receipt", "", "--output-dir", "/out"},
	}
	for _, args := range cases {
		if _, ok := parseArgs(args); ok {
			t.Errorf("parseArgs(%q) unexpectedly accepted", args)
		}
	}
}

func TestRunPrintsUsageForMalformedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--repository"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() code = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: stage-a0-policy-candidate") {
		t.Fatalf("run() output = stdout %q stderr %q", stdout.String(), stderr.String())
	}
}
