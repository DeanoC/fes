//go:build !linux || !fpgadev

package main

import (
	"bytes"
	"testing"
)

func TestUnsupportedInstallCommandUsesUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if got := runInstallCommand([]string{"install-profile"}, &out, &errOut, nil); got != exitUsage || out.Len() != 0 || errOut.Len() == 0 {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
}
