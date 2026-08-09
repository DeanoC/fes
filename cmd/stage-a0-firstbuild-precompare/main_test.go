package main

import (
	"bytes"
	"testing"
)

func TestParseArgsRequiresAllLocations(t *testing.T) {
	if _, ok := parseArgs(nil); ok {
		t.Fatal("parseArgs accepted missing locations")
	}
	req, ok := parseArgs([]string{"--left", "/left", "--right", "/right", "--report", "/report"})
	if !ok {
		t.Fatal("parseArgs rejected complete request")
	}
	if req.left != "/left" || req.right != "/right" || req.report != "/report" {
		t.Fatalf("request = %#v", req)
	}
}

func TestRunRejectsUnknownArgumentsWithoutLeakingValues(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"--left", "/private/secret", "--bogus", "value"}, &stdout, &stderr); got != exitUsage {
		t.Fatalf("run() = %d, want %d", got, exitUsage)
	}
	if stdout.Len() != 0 || stderr.String() != usageText || bytes.Contains(stderr.Bytes(), []byte("secret")) {
		t.Fatalf("usage output = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunReportsStableFailureCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"--left", "/left", "--right", "/right", "--report", "/report"}, &stdout, &stderr); got != exitCompare {
		t.Fatalf("run() = %d, want %d", got, exitCompare)
	}
	if stdout.Len() != 0 || stderr.String() != "PRECOMPARE_RECEIPT_INVALID: capture root is missing or unsafe\n" {
		t.Fatalf("failure output = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
