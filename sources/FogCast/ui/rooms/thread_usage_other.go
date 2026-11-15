//go:build !linux

package rooms

import "time"

// readThreadUsage has no per-thread source here; timings report wall time only.
func readThreadUsage() (threadUsage, bool) { return threadUsage{}, false }

// threadCPUSupported says currentThread reads no thread CPU time here.
const threadCPUSupported = false

// currentThread has no per-thread source here.
func currentThread() (tid int, cpu time.Duration, ok bool) { return 0, 0, false }

// schedstatReader has no run-queue source here.
type schedstatReader struct{}

func (*schedstatReader) close() {}

func (*schedstatReader) runDelay() (time.Duration, bool) { return 0, false }
