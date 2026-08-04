package main

import (
	"bytes"
	"testing"
)

func TestRecordAndVerifyCommandsRequireExactArguments(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		nil,
		{"record"},
		{"verify"},
		{"future"},
		{"record", "--poc1a-lock", "a", "--poc1b-lock", "b", "--prod", "p", "--dev", "d", "--output", "o", "extra"},
		{"verify", "--lock", "l", "--poc1a-lock", "a", "--poc1b-lock", "b", "--prod", "p", "--dev", "d", "extra"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%q) = %d, want usage failure; stderr=%q", args, code, stderr.String())
		}
	}
}
