package main

import (
	"bytes"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
)

func TestParseArgsRequiresOperationalLocations(t *testing.T) {
	if _, ok := parseArgs(nil); ok {
		t.Fatal("parseArgs accepted missing locations")
	}
	request, ok := parseArgs([]string{"--source", "/src", "--toolchain-archive", "/tc.tar.xz", "--output", "/out"})
	if !ok {
		t.Fatal("parseArgs rejected complete request")
	}
	if request.ImageRef != firstbuild.ExpectedContainerReference {
		t.Fatalf("parseArgs did not retain locked defaults: %#v", request)
	}
}

func TestRunRejectsUnknownArgumentsWithoutLeakingValues(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"--source", "/private/secret", "--bogus", "value"}, &stdout, &stderr); got != exitUsage {
		t.Fatalf("run() = %d, want %d", got, exitUsage)
	}
	if stdout.Len() != 0 || stderr.String() != usageText || bytes.Contains(stderr.Bytes(), []byte("secret")) {
		t.Fatalf("usage output = stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
