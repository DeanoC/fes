package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsNonLoopbackListenAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--listen", "0.0.0.0:8787"}, &stdout, &stderr, nil)
	if code != 2 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestNormalizeListenAddressAcceptsLoopbackForms(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		if got, err := normalizeListenAddress(address); err != nil || got == "" {
			t.Errorf("normalize %q = %q, %v", address, got, err)
		}
	}
}
