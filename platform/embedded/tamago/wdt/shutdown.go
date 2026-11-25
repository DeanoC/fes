// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/psci"
)

// ShutdownPrepareStep returns a bounded psci.ShutdownStep that extends or kicks
// an armed watchdog without stopping the keepalive supervisor, failing closed
// if minTimeout cannot be satisfied by hardware. Keeping the supervisor active
// ensures the sticky hardware watchdog stays serviced if psci.Shutdown aborts
// or if the terminal PSCI call returns an error. An unarmed or nil supervisor
// stays untouched.
func ShutdownPrepareStep(sup *Supervisor, minTimeout time.Duration) psci.ShutdownStep {
	return psci.ShutdownStep{
		Name: "wdt-prepare-shutdown",
		Fn: func(context.Context) error {
			if minTimeout < 0 {
				return fmt.Errorf("%w: shutdown minTimeout must be >= 0", ErrInvalidConfig)
			}
			if sup == nil || !sup.Started() {
				return nil
			}
			if minTimeout > 0 {
				actual, err := sup.Extend(minTimeout)
				if errors.Is(err, ErrUnsupported) {
					if kickErr := sup.KickNow(); kickErr != nil {
						return kickErr
					}
					if sup.Period() < minTimeout {
						return fmt.Errorf("%w: watchdog period %v below requested shutdown minTimeout %v", ErrUnsupported, sup.Period(), minTimeout)
					}
					return nil
				}
				if err != nil {
					return err
				}
				if actual < minTimeout {
					return fmt.Errorf("%w: achieved shutdown timeout %v below requested %v", ErrInvalidConfig, actual, minTimeout)
				}
				return nil
			}
			return sup.KickNow()
		},
	}
}

type shutdownPowerController struct {
	sup  *Supervisor
	ctrl psci.PowerController
}

// WrapPowerController wraps ctrl so an armed Supervisor kicks the hardware
// watchdog immediately before SystemReset or SystemOff runs while keeping
// background keepalive active in case PSCI fails and returns instead of resetting
// or powering off. Passing nil ctrl returns nil.
func WrapPowerController(sup *Supervisor, ctrl psci.PowerController) psci.PowerController {
	if ctrl == nil || sup == nil {
		return ctrl
	}
	return &shutdownPowerController{sup: sup, ctrl: ctrl}
}

func (w *shutdownPowerController) kickIfStarted() {
	if w.sup.Started() {
		_ = w.sup.KickNow()
	}
}

func (w *shutdownPowerController) SystemReset() error {
	w.kickIfStarted()
	return w.ctrl.SystemReset()
}

func (w *shutdownPowerController) SystemOff() error {
	w.kickIfStarted()
	return w.ctrl.SystemOff()
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
