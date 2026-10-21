//go:build !linux

package rooms

// readThreadUsage has no per-thread source here; timings report wall time only.
func readThreadUsage() (threadUsage, bool) { return threadUsage{}, false }
