//go:build !linux || !fpgadev

package main

import (
	"io"
)

// Unsupported builds keep the dispatcher total without exposing mutation
// grammar or target lifecycle names in the production binary.
func installCommandName(string) bool { return false }

func runInstallCommand(_ []string, _ io.Writer, stderr io.Writer, _ commandRunner) int {
	return usage(stderr)
}
