package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
)

// Controller must verify that all former processes have exited before returning
// from Stop. Healthy verifies exact executable identities, native idle and actual
// launcher readiness. It must not treat process existence alone as readiness.
// Verify wraps software.ErrIncompatible for a release this base cannot run.
// Active is the release whose group was last started, or empty after Stop.
type Controller interface {
	Verify(context.Context, string) error
	Stop(context.Context) error
	Start(context.Context, string, Request) error
	Healthy(context.Context, string) error
	Active() string
}

// Admission owns the target lease reservation. Acquire never takes over a live
// execution owner. It is idempotent across agent restart and supervisor restart.
// Release is called only after durable completion, including failed-candidate
// recovery. Errors must retain the reservation and block execution.
type Admission interface {
	Acquire(context.Context, Request) error
	Release(context.Context, Request) error
}

type Engine struct {
	Repository Repository
	Controller Controller
	Admission  Admission
	// Gate rejects activation during appliance trial/pending/corrupt state.
	Gate func(context.Context) error
	// Factory is this image's own software. It runs in place of a selection
	// the image cannot run; empty keeps such a selection's verification error.
	Factory string
	// HeldBack is the committed selection this image cannot run, and
	// HeldBackReason says why. The selection stays recorded for an image that
	// can run it.
	HeldBack, HeldBackReason string
	// Defaults are 60 seconds deadline, 10 seconds uninterrupted readiness and
	// 250 milliseconds polling. Tests can use a virtual clock.
	Deadline, Stable, Poll time.Duration
	Now                    func() time.Time
	Wait                   func(context.Context, time.Duration) error
}

func (e *Engine) defaults() {
	if e.Deadline == 0 {
		e.Deadline = 60 * time.Second
	}
	if e.Stable == 0 {
		e.Stable = 10 * time.Second
	}
	if e.Poll == 0 {
		e.Poll = 250 * time.Millisecond
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Wait == nil {
		e.Wait = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
}

func (e *Engine) health(ctx context.Context, id string, deadline time.Time) error {
	var since time.Time
	var last error
	for e.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		probeCtx, cancel := context.WithTimeout(ctx, e.Poll*4)
		err := e.Controller.Healthy(probeCtx, id)
		cancel()
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if !e.Now().Before(deadline) {
			break
		}
		if err == nil {
			if since.IsZero() {
				since = e.Now()
			}
			if e.Now().Sub(since) >= e.Stable {
				return nil
			}
		} else {
			since = time.Time{}
			last = err
			// A dead process will not become ready by waiting. Let startup
			// replace it before the readiness deadline.
			if errors.Is(err, ErrProcessExited) {
				return err
			}
		}
		if err := e.Wait(ctx, e.Poll); err != nil {
			return err
		}
	}
	return fmt.Errorf("software readiness deadline exceeded: %w", errors.Join(last, context.DeadlineExceeded))
}

func (e *Engine) switchTo(ctx context.Context, id string, request Request) error {
	// Hashing immutable release bytes is preparation, not service startup. Keep
	// the existing group running while it completes and honor caller cancellation.
	if err := e.Controller.Verify(ctx, id); err != nil {
		return err
	}
	return e.switchPrepared(ctx, id, request)
}

// prepareSelection verifies the release this image runs for the committed
// selection. A selection the image cannot run is held back and the image's
// factory software is prepared in its place. Other failures are returned.
func (e *Engine) prepareSelection(ctx context.Context, id string) (string, error) {
	err := e.Controller.Verify(ctx, id)
	if err == nil {
		e.HeldBack, e.HeldBackReason = "", ""
		return id, nil
	}
	if !errors.Is(err, software.ErrIncompatible) || e.Factory == "" || id == e.Factory {
		return id, err
	}
	e.HeldBack, e.HeldBackReason = id, err.Error()
	return e.Factory, e.Controller.Verify(ctx, e.Factory)
}

func (e *Engine) switchPrepared(ctx context.Context, id string, request Request) error {
	ctx, cancel := context.WithTimeout(ctx, e.Deadline)
	defer cancel()
	deadline := e.Now().Add(e.Deadline)
	if err := e.Controller.Stop(ctx); err != nil {
		return fmt.Errorf("old processes did not exit: %w", err)
	}
	// Restart only until the first accepted readiness window. An exit during
	// native initialization and an exit observed by health share this deadline.
	// An exit after health returns is left for the supervisor tick. That tick
	// restarts a dead launcher without stopping the runtime or agent, and
	// restarts the whole group when the runtime or agent exits. The replacement
	// stays in recovery until this same readiness check holds. A runtime that
	// is already executing a finished session is accepted once the recovery
	// fence is confirmed. A confirmed session whose fence cannot be closed
	// stays running, and new execution claims stay closed until the group is
	// accepted. Every other Start error ends the attempt unchanged.
	var last error
	for e.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := e.Controller.Start(ctx, id, request)
		if err == nil {
			err = e.health(ctx, id, deadline)
		}
		if err == nil || !errors.Is(err, ErrProcessExited) {
			return err
		}
		last = err
		if err := e.Controller.Stop(ctx); err != nil {
			return fmt.Errorf("old processes did not exit: %w", err)
		}
		if err := e.Wait(ctx, e.Poll); err != nil {
			return err
		}
	}
	return fmt.Errorf("software readiness deadline exceeded: %w", errors.Join(last, context.DeadlineExceeded))
}

// Run advances at most one durable request. A process-wide worker lock prevents
// a concurrent poller mistaking a live activation for an interrupted one.
func (e *Engine) Run(ctx context.Context) error {
	unlock, err := e.Repository.WorkerLock()
	if err != nil {
		return err
	}
	defer unlock()
	e.defaults()
	s, err := e.Repository.Read()
	if err != nil {
		return err
	}
	if s.Unreleased != nil {
		return e.release(ctx, *s.Unreleased)
	}
	if s.Pending == nil {
		return nil
	}
	op := *s.Pending
	if op.Phase == "blocked" {
		return fmt.Errorf("software activation blocked: %s", op.Reason)
	}
	if e.Gate != nil {
		if err := e.Gate(ctx); err != nil {
			if op.Phase == "queued" {
				_ = transition(e.Repository, op, "queued", "queued", err.Error())
			}
			return err
		}
	}
	if err := e.Admission.Acquire(ctx, op.Request); err != nil {
		if op.Phase == "queued" {
			_ = transition(e.Repository, op, "queued", "queued", err.Error())
		}
		return err
	}
	if op.Phase == "recovering" {
		reason := "supervisor interrupted after recovery attempt was consumed"
		if err := transition(e.Repository, op, "recovering", "blocked", reason); err != nil {
			return err
		}
		return errors.New(reason)
	}
	reason := "interrupted software activation"
	if op.Phase == "queued" {
		err := e.Controller.Verify(ctx, op.Candidate)
		switch {
		case errors.Is(err, software.ErrIncompatible):
			return e.holdBack(ctx, op, "held back: "+err.Error())
		case err != nil:
			// No service was stopped. Preserve a diagnosable blocked request and its
			// reservation rather than repeatedly attempting an invalid artifact.
			if save := transition(e.Repository, op, "queued", "blocked", err.Error()); save != nil {
				return save
			}
			return err
		default:
			if err := transition(e.Repository, op, "queued", "applying", ""); err != nil {
				return err
			}
			err = e.switchPrepared(ctx, op.Candidate, op.Request)
			if err == nil {
				if err = complete(e.Repository, op, "applying", "accepted", ""); err != nil {
					return err
				}
				e.HeldBack, e.HeldBackReason = "", ""
				return e.release(ctx, op.Request)
			}
			reason = err.Error()
		}
	}
	// A shutdown during candidate activation leaves applying durable. Spending
	// recovery with a cancelled context would prevent the next boot recovering.
	if err := ctx.Err(); err != nil {
		return err
	}
	// Persist the spent recovery attempt BEFORE stopping or starting anything.
	if err := transition(e.Repository, op, "applying", "recovering", reason); err != nil {
		return err
	}
	// The selection is the recovery target. If this image cannot run it, the
	// factory software takes its place and the selection is held back.
	id, err := e.prepareSelection(ctx, op.Previous)
	if err == nil {
		err = e.switchPrepared(ctx, id, op.Request)
	}
	if err != nil {
		failure := fmt.Sprintf("candidate failed (%s); recovery failed: %v", reason, err)
		if save := transition(e.Repository, op, "recovering", "blocked", failure); save != nil {
			return save
		}
		return errors.New(failure)
	}
	if err := complete(e.Repository, op, "recovering", "recovered", reason); err != nil {
		return err
	}
	return e.release(ctx, op.Request)
}

// holdBack completes a queued activation whose candidate this image cannot
// run. The candidate never starts and no recovery attempt is spent. The
// selection keeps running and no service is touched, unless something else
// runs, as the factory software still does after an image change. Then the
// selection starts first while the request stays queued, so an interruption
// leaves Boot to start the selection exactly as it would anyway. Only a
// selection that fails to start blocks. The receipt reuses "recovered" so a
// supervisor from an older image can still read this state after a rollback.
func (e *Engine) holdBack(ctx context.Context, op Operation, reason string) error {
	id, err := e.prepareSelection(ctx, op.Previous)
	if err == nil && id != e.Controller.Active() {
		err = e.switchPrepared(ctx, id, op.Request)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		failure := fmt.Sprintf("%s; starting the selection failed: %v", reason, err)
		if save := transition(e.Repository, op, "queued", "blocked", failure); save != nil {
			return save
		}
		return errors.New(failure)
	}
	if err := complete(e.Repository, op, "queued", "recovered", reason); err != nil {
		return err
	}
	return e.release(ctx, op.Request)
}

func (e *Engine) release(ctx context.Context, req Request) error {
	if err := e.Admission.Release(ctx, req); err != nil {
		return err
	}
	return e.Repository.Update(func(s *State) error {
		if s.Unreleased == nil || *s.Unreleased != req {
			return ErrConflict
		}
		s.Unreleased = nil
		return nil
	})
}

// Boot starts the committed selection while the agent queues admission before
// exposing sessions. Interrupted candidate activation consumes its one recovery
// before starting the prior release. A spent recovery never starts again.
func (e *Engine) Boot(ctx context.Context, boot Request) error {
	unlock, err := e.Repository.WorkerLock()
	if err != nil {
		return err
	}
	defer unlock()
	e.defaults()
	s, err := e.Repository.Read()
	if err != nil {
		return err
	}
	if s.Pending != nil && (s.Pending.Phase == "recovering" || s.Pending.Phase == "blocked") {
		if s.Pending.Phase == "recovering" {
			if err := transition(e.Repository, *s.Pending, "recovering", "blocked", "recovery interrupted by reboot"); err != nil {
				return err
			}
		}
		return errors.New("software recovery exhausted; operator intervention required")
	}
	req := boot
	if s.Pending != nil {
		req = s.Pending.Request
	} else if s.Unreleased != nil {
		req = *s.Unreleased
	}
	recovering := s.Pending != nil && s.Pending.Phase == "applying"
	if recovering {
		if err := transition(e.Repository, *s.Pending, "applying", "recovering", "activation interrupted by reboot"); err != nil {
			return err
		}
	}
	// A selection this image cannot run is held back, and the image's factory
	// software starts in its place instead of blocking startup.
	id, err := e.prepareSelection(ctx, s.Selected)
	if err == nil {
		err = e.switchPrepared(ctx, id, req)
	}
	if err != nil {
		if recovering {
			_ = transition(e.Repository, *s.Pending, "recovering", "blocked", err.Error())
		}
		return err
	}
	if err := e.Admission.Acquire(ctx, req); err != nil {
		return err
	}
	if recovering {
		if err := complete(e.Repository, *s.Pending, "recovering", "recovered", "activation interrupted by reboot"); err != nil {
			return err
		}
		return e.release(ctx, req)
	}
	if s.Unreleased != nil {
		return e.release(ctx, req)
	}
	if s.Pending != nil {
		return nil
	}
	return e.Admission.Release(ctx, req)
}

// StartFactory uses the same verified preparation and bounded startup as a
// selected release. Appliance-trial startup deliberately creates no software
// maintenance reservation so the OS confirmation lease remains available.
func (e *Engine) StartFactory(ctx context.Context, id string) error {
	e.defaults()
	return e.switchTo(ctx, id, Request{})
}
