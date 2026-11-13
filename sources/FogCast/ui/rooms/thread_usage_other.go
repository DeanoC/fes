//go:build !linux

package rooms

// readThreadUsage has no per-thread source here; timings report wall time only.
func readThreadUsage(*schedstatReader) (threadUsage, bool) { return threadUsage{}, false }

// schedstatReader has no run-queue source here.
type schedstatReader struct{}

func (*schedstatReader) close() {}
