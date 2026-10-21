//go:build linux

package rooms

import (
	"time"

	"golang.org/x/sys/unix"
)

// readThreadUsage samples the calling OS thread. The caller keeps its
// goroutine locked to that thread between samples.
func readThreadUsage() (threadUsage, bool) {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts) != nil {
		return threadUsage{}, false
	}
	var ru unix.Rusage
	if unix.Getrusage(unix.RUSAGE_THREAD, &ru) != nil {
		return threadUsage{}, false
	}
	return threadUsage{
		cpu:         time.Duration(ts.Nano()),
		voluntary:   int64(ru.Nvcsw),
		involuntary: int64(ru.Nivcsw),
	}, true
}
