//go:build !linux || !fpgadev

package main

import (
	"io"
)

func faultCommandName(string) bool { return false }

func runFaultCommand(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	return usage(stderr)
}
