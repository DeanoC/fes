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
	ActionReset     PowerAction = 1
	ActionPowerOff  PowerAction = 2
	ActionWarmReset PowerAction = 3
)

func (a PowerAction) String() string {
	switch a {
	case ActionReset:
		return "reset"
	case ActionPowerOff:
		return "poweroff"
	case ActionWarmReset:
		return "warm-reset"
	default:
		return fmt.Sprintf("PowerAction(%d)", uint8(a))
	}
}

const defaultShutdownStepTimeout = 5 * time.Second

var (
	// ErrInvalidShutdown is returned when Shutdown receives invalid arguments or
	// step definitions before executing any teardown step or PSCI call.
	ErrInvalidShutdown = errors.New("psci: invalid shutdown configuration")
	// ErrShutdownAborted wraps ctx.Err() (and any step errors so far) when ctx
	// is done before Shutdown issues the terminal PSCI transition. Once Shutdown
	// sees ctx done it starts no further step and does not call PSCI, so the
	// system stays up with only the already completed steps applied.
	ErrShutdownAborted = errors.New("psci: shutdown aborted")
	// ErrTeardownFailed wraps teardown errors when RequireCleanTeardown prevents
	// issuing the terminal PSCI transition.
	ErrTeardownFailed = errors.New("psci: shutdown teardown failed")
)

// PowerController abstracts the terminal PSCI power transition (satisfied by *Client).
type PowerController interface {
	SystemReset() error
	SystemOff() error
}

// WarmResetController is an optional extension implemented by PowerController
// implementations (such as *Client) that can advertise and execute
// PSCI_SYSTEM_RESET2 architectural warm resets.
type WarmResetController interface {
	SupportsWarmReset() bool
	SystemReset2(resetType uint32, cookie uint64) error
}

var (
	_ PowerController     = (*Client)(nil)
	_ WarmResetController = (*Client)(nil)
)

// ShutdownStep is one bounded, named containment/flush step executed before PSCI reset or poweroff.
type ShutdownStep struct {
	Name string
	Fn   func(context.Context) error
}

// ShutdownConfig controls how Shutdown runs ordered teardown steps before PSCI.
type ShutdownConfig struct {
	Steps                []ShutdownStep
	StepTimeout          time.Duration // Per-step deadline and longest wait for a step (default 5s; must be >= 0)
	OnStepError          func(name string, err error)
	RequireCleanTeardown bool // When true, abort without calling PSCI if any step fails
}

// Shutdown runs cfg.Steps strictly in order, then invokes ctrl.SystemReset
// (ActionReset), ctrl.SystemOff (ActionPowerOff), or WarmResetController.SystemReset2(0, 0)
// (ActionWarmReset).
//
// Each step runs on its own goroutine with a context that ends when ctx is done
// or cfg.StepTimeout elapses, and Shutdown waits for the step to return before
// starting the next step or calling PSCI. A step panic is recovered and recorded
// so later hardware containment steps still run. A step that has not returned
// when its StepTimeout elapses is abandoned (left running) and recorded as
// context.DeadlineExceeded, so a hook that ignores its context cannot stall
// shutdown; that is the only way a step outlives Shutdown.
//
// ctx cancels the shutdown itself. Shutdown checks it before every step and
// before PSCI; once ctx is done it starts no further step, does not call PSCI,
// and returns ErrShutdownAborted wrapping ctx.Err() and any step errors. A step
// already running when ctx ends sees its own context end and is awaited as
// above first.
//
// Step errors go to cfg.OnStepError. With RequireCleanTeardown any step error
// returns ErrTeardownFailed without calling PSCI; otherwise PSCI still runs and
// its error is joined with the step errors.
func Shutdown(ctx context.Context, ctrl PowerController, action PowerAction, cfg ShutdownConfig) error {
	if ctx == nil || ctrl == nil {
		return ErrInvalidShutdown
	}
	var warmCtrl WarmResetController
	switch action {
	case ActionReset, ActionPowerOff:
	case ActionWarmReset:
		var ok bool
		warmCtrl, ok = ctrl.(WarmResetController)
		if !ok {
			return fmt.Errorf("%w: %w", ErrInvalidShutdown, ErrUnsupported)
		}
	default:
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
		next := "PSCI " + action.String()
		if len(cfg.Steps) > 0 {
			next = fmt.Sprintf("step %q", cfg.Steps[0].Name)
		}
		return abortShutdown(next, err, nil)
	}
	if warmCtrl != nil && !warmCtrl.SupportsWarmReset() {
		return fmt.Errorf("%w: %w", ErrInvalidShutdown, ErrUnsupported)
	}

	timeout := cfg.StepTimeout
	if timeout == 0 {
		timeout = defaultShutdownStepTimeout
	}

	var stepErrs []error
	for _, step := range cfg.Steps {
		// ctx ending cancels the shutdown: start no further step, skip PSCI.
		if err := ctx.Err(); err != nil {
			return abortShutdown(fmt.Sprintf("step %q", step.Name), err, stepErrs)
		}
		err := runShutdownStep(ctx, timeout, step)
		if err != nil {
			wrapped := fmt.Errorf("%s: %w", step.Name, err)
			stepErrs = append(stepErrs, wrapped)
			if cfg.OnStepError != nil {
				cfg.OnStepError(step.Name, err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return abortShutdown("PSCI "+action.String(), err, stepErrs)
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
	case ActionWarmReset:
		psciErr = warmCtrl.SystemReset2(0, 0)
	}
	if psciErr != nil {
		stepErrs = append(stepErrs, psciErr)
	}
	return errors.Join(stepErrs...)
}

// abortShutdown reports that ctx ended before next could run; no PSCI call follows.
func abortShutdown(next string, ctxErr error, stepErrs []error) error {
	return fmt.Errorf("%w before %s: %w", ErrShutdownAborted, next, errors.Join(append([]error{ctxErr}, stepErrs...)...))
}

// runShutdownStep runs step.Fn on its own goroutine so a hook that ignores its
// context cannot stall shutdown. The step's context ends when parent is done or
// timeout elapses, but only timeout ends the wait: a step interrupted by parent
// is still awaited, so it never overlaps the next step, PSCI, or Shutdown's
// return. A step that has not returned when timeout elapses is abandoned.
func runShutdownStep(parent context.Context, timeout time.Duration, step ShutdownStep) error {
	stepCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	wait := time.NewTimer(timeout)
	defer wait.Stop()

	done := make(chan error, 1) // buffered: an abandoned step must not block on send
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
	case <-wait.C:
		return context.DeadlineExceeded
	}
}
