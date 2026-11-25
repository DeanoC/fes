// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/psci"
)

// ShutdownPrepareStep returns a bounded psci.ShutdownStep that extends/kicks
// an armed watchdog BEFORE draining the background supervisor (so an in-flight
// Check/OnError callback cannot let hardware expire while waiting for Stop),
// stops periodic keepalive checks, and fails closed if minTimeout cannot be
// satisfied by hardware. An unarmed or nil supervisor stays untouched.
func ShutdownPrepareStep(sup *Supervisor, minTimeout time.Duration) psci.ShutdownStep {
	return psci.ShutdownStep{
		Name: "wdt-prepare-shutdown",
		Fn: func(context.Context) error {
			if minTimeout < 0 {
				return fmt.Errorf("%w: shutdown minTimeout must be >= 0", ErrInvalidConfig)
			}
			if sup == nil {
				return nil
			}
			if !sup.Started() {
				sup.Stop()
				return nil
			}
			var prepErr error
			if minTimeout > 0 {
				actual, err := sup.Extend(minTimeout)
				if errors.Is(err, ErrUnsupported) {
					prepErr = sup.KickNow()
					if prepErr == nil && sup.Period() < minTimeout {
						prepErr = fmt.Errorf("%w: watchdog period %v below requested shutdown minTimeout %v", ErrUnsupported, sup.Period(), minTimeout)
					}
				} else if err != nil {
					prepErr = err
				} else if actual < minTimeout {
					prepErr = fmt.Errorf("%w: achieved shutdown timeout %v below requested %v", ErrInvalidConfig, actual, minTimeout)
				}
			} else {
				prepErr = sup.KickNow()
			}
			sup.Stop()
			return prepErr
		},
	}
}

// WrapShutdownSteps wraps each valid step in steps so an armed Supervisor kicks
// the sticky hardware watchdog before the step runs and (while ctx remains active)
// after the step finishes, keeping slow quiesce/flush steps from tripping reset.
// Invalid steps (empty Name or nil Fn) are left unwrapped so psci.Shutdown still
// rejects them with ErrInvalidShutdown.
func WrapShutdownSteps(sup *Supervisor, steps []psci.ShutdownStep) []psci.ShutdownStep {
	wrapped := make([]psci.ShutdownStep, len(steps))
	for i, step := range steps {
		if step.Name == "" || step.Fn == nil {
			wrapped[i] = step
			continue
		}
		orig := step
		wrapped[i] = psci.ShutdownStep{
			Name: orig.Name,
			Fn: func(ctx context.Context) error {
				var preErr error
				if sup != nil && sup.Started() {
					if err := sup.KickNow(); err != nil && !errors.Is(err, ErrNotRunning) {
						preErr = err
					}
				}
				stepErr := orig.Fn(ctx)
				var postErr error
				if sup != nil && sup.Started() && ctx.Err() == nil {
					if err := sup.KickNow(); err != nil && !errors.Is(err, ErrNotRunning) {
						postErr = err
					}
				}
				return errors.Join(preErr, stepErr, postErr)
			},
		}
	}
	return wrapped
}
