// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"
	"time"
)

// Samples exactly the caller's existing pre/post-Advance clock boundaries.
// Phase totals are observations, never authority to extend or restart a bound.
type configurationClock struct {
	now         func() time.Time
	scope       string
	start, last time.Time
	phases      [6]time.Duration
}

func newConfigurationClock(now func() time.Time, scope string) configurationClock {
	start := now()
	return configurationClock{now: now, scope: scope, start: start, last: start}
}
func configurationPhase(phase string) int {
	switch phase {
	case "start":
		return 0
	case "write":
		return 1
	case "completion":
		return 2
	case "done":
		return 3
	case "retire":
		return 4
	default:
		return 5
	}
}
func (c *configurationClock) check(ctx context.Context, transfer *ConfigurationTransfer, phase string, after bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: configuration context: phase=%s->%s completed=%t: %w", c.scope, phase, transfer.phase, transfer.done, err)
	}
	now := c.now()
	elapsed, step := now.Sub(c.start), now.Sub(c.last)
	if after && step >= 0 {
		c.phases[configurationPhase(phase)] += step
	}
	reason := ""
	if now.Before(c.last) {
		reason = "backwards clock"
	} else if elapsed >= sourceOperationTimeout {
		reason = "deadline"
	}
	if reason != "" {
		return fmt.Errorf("%s: configuration %s: elapsed=%s step=%s limit=%s phase=%s->%s completed=%t phase_times={start=%s write=%s completion=%s done=%s retire=%s other=%s}", c.scope, reason, elapsed, step, sourceOperationTimeout, phase, transfer.phase, transfer.done, c.phases[0], c.phases[1], c.phases[2], c.phases[3], c.phases[4], c.phases[5])
	}
	c.last = now
	return nil
}
