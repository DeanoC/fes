//go:build !linux

package rooms

import "time"

// readThreadUsage has no per-thread source here; timings report wall time only.
func readThreadUsage(*threadUsage) bool { return false }

// schedstatReader has no run-queue source here.
type schedstatReader struct{}

func (*schedstatReader) close() {}

func (*schedstatReader) runDelay() (time.Duration, bool) { return 0, false }
