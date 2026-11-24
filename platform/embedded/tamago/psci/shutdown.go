// SPDX-License-Identifier: GPL-3.0-or-later

package psci

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PowerAction selects the terminal ARM PSCI system transition executed after
// coordinated teardown steps finish.
type PowerAction uint8

const (
	ActionReset    PowerAction = 1
	ActionPowerOff PowerAction = 2
)

func (a PowerAction) String() string {
	switch a {
	case ActionReset:
		return "reset"
	case ActionPowerOff:
		return "poweroff"
	default:
		return fmt.Sprintf("PowerAction(%d)", uint8(a))
	}
}

const defaultShutdownStepTimeout = 5 * time.Second

var (
	// ErrInvalidShutdown is returned when Shutdown receives invalid arguments or
	// step definitions before executing any teardown step or PSCI call.
	ErrInvalidShutdown = errors.New("psci: invalid shutdown configuration")
	// ErrTeardownFailed wraps teardown errors when RequireCleanTeardown prevents
	// issuing the terminal PSCI transition.
	ErrTeardownFailed = errors.New("psci: shutdown teardown failed")
)

// PowerController abstracts the terminal PSCI power transition (satisfied by *Client).
type PowerController interface {
	SystemReset() error
	SystemOff() error
}

var _ PowerController = (*Client)(nil)

// ShutdownStep is one bounded, named containment/flush step executed before PSCI reset or poweroff.
type ShutdownStep struct {
	Name string
	Fn   func(context.Context) error
}

// ShutdownConfig controls how Shutdown runs ordered teardown steps before PSCI.
type ShutdownConfig struct {
	Steps                []ShutdownStep
	StepTimeout          time.Duration // Per-step deadline (default 5s; must be >= 0)
	OnStepError          func(name string, err error)
	RequireCleanTeardown bool // When true, abort without calling PSCI if any step fails
}

// Shutdown runs cfg.Steps in order, bounding each step by cfg.StepTimeout (and
// parent ctx), recovering any panic so subsequent hardware containment steps
// still run. Unless RequireCleanTeardown rejects due to a failed step, it then
// invokes ctrl.SystemReset (ActionReset) or ctrl.SystemOff (ActionPowerOff).
func Shutdown(ctx context.Context, ctrl PowerController, action PowerAction, cfg ShutdownConfig) error {
	if ctx == nil || ctrl == nil {
		return ErrInvalidShutdown
	}
	if action != ActionReset && action != ActionPowerOff {
		return fmt.Errorf("%w: unknown action %v", ErrInvalidShutdown, action)
	}
	if cfg.StepTimeout < 0 {
		return fmt.Errorf("%w: negative step timeout %s", ErrInvalidShutdown, cfg.StepTimeout)
	}
	for i, step := range cfg.Steps {
		if step.Name == "" || step.Fn == nil {
			return fmt.Errorf("%w: invalid step at index %d", ErrInvalidShutdown, i)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	timeout := cfg.StepTimeout
	if timeout == 0 {
		timeout = defaultShutdownStepTimeout
	}

	var stepErrs []error
	for _, step := range cfg.Steps {
		err := runShutdownStep(ctx, timeout, step)
		if err != nil {
			wrapped := fmt.Errorf("%s: %w", step.Name, err)
			stepErrs = append(stepErrs, wrapped)
			if cfg.OnStepError != nil {
				cfg.OnStepError(step.Name, err)
			}
		}
	}

	if len(stepErrs) > 0 && cfg.RequireCleanTeardown {
		return fmt.Errorf("%w: %w", ErrTeardownFailed, errors.Join(stepErrs...))
	}

	var psciErr error
	switch action {
	case ActionReset:
		psciErr = ctrl.SystemReset()
	case ActionPowerOff:
		psciErr = ctrl.SystemOff()
	}
	if psciErr != nil {
		stepErrs = append(stepErrs, psciErr)
	}
	return errors.Join(stepErrs...)
}

func runShutdownStep(parent context.Context, timeout time.Duration, step ShutdownStep) error {
	stepCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- step.Fn(stepCtx)
	}()

	select {
	case err := <-done:
		return err
	case <-stepCtx.Done():
		return stepCtx.Err()
	}
}
