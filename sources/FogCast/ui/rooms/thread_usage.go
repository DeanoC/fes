package rooms

import (
	"fmt"
	"time"
)

// threadUsage is one OS thread's CPU time and context switches.
type threadUsage struct {
	cpu         time.Duration
	voluntary   int64
	involuntary int64
}

func (u threadUsage) since(before threadUsage) threadUsage {
	return threadUsage{
		cpu:         u.cpu - before.cpu,
		voluntary:   u.voluntary - before.voluntary,
		involuntary: u.involuntary - before.involuntary,
	}
}

func (u threadUsage) String() string {
	return fmt.Sprintf("thread cpu %s, %d voluntary/%d involuntary switches",
		u.cpu.Round(10*time.Microsecond), u.voluntary, u.involuntary)
}
