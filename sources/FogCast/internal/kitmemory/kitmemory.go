// Package kitmemory sets the soft Go memory budgets of the kit's processes.
package kitmemory

import (
	"os"
	"runtime/debug"
)

// Budgets on the 492 MiB board, which the runtime and the running core's
// staged files in tmpfs share. A process may pass its budget briefly, for
// example while decoding artwork or linking a ROM; the Go runtime then
// collects and scavenges sooner instead of keeping a peak-sized heap.
const (
	Launcher = 96 << 20
	Agent    = 64 << 20
	// Host is fogcast-kit host. It peaks near 80 MiB during an ST launch
	// and near 140 MiB while titling one (#863 slices 2-4).
	Host = 96 << 20
)

// SetLimit applies limit unless GOMEMLIMIT is set, which wins. Call it from a
// kit entry point before starting workers.
func SetLimit(limit int64) {
	if _, configured := os.LookupEnv("GOMEMLIMIT"); !configured {
		debug.SetMemoryLimit(limit)
	}
}
