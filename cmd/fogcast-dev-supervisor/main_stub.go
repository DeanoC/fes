//go:build !linux || !fpgadev

package main

import (
	"context"
	"io"
)

const (
	supervisorExitOK      = 0
	supervisorExitFailure = 1
	supervisorExitUsage   = 2
)

func main() {}

// Unsupported builds retain a package entry point for cross-platform tests;
// they do not expose target lifecycle grammar or call into the runtime.
func runSupervisor([]string, io.Writer, io.Writer, func(context.Context, any) error) int {
	return supervisorExitUsage
}
