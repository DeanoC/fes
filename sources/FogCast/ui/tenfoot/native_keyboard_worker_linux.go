//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

// nativeKeyboardAwaitRetirement is called after quarantine while the actual
// worker remains live. Idle is reached only by a confirmed reset or a BEGIN
// proven unsent; ambiguous transport outcomes remain unavailable.
func (a *App) nativeKeyboardAwaitRetirement(ctx context.Context) error {
	if a == nil || ctx == nil {
		return errNativeGuestState
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		phase := a.nativeGuest.phase
		beginPending := a.nativeGuestBeginRequest != nil
		padRetiring := a.nativePadRetiring
		a.mu.Unlock()
		if phase == nativeGuestUncertain {
			return errNativeGuestState
		}
		if phase == nativeGuestIdle && !beginPending && !padRetiring {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// nativeKeyboardRunOne is an inert, synchronous worker step. observe reads
// current App authority; send must return a transport-correlated ACK. Neither
// is installed or started here. An unavailable authority still permits the
// exact old-source reset, but cannot authorize a new BEGIN or edge.
func (a *App) nativeKeyboardRunOne(ctx context.Context, expected nativeGuestBinding, observe func() (nativeGuestBinding, error), send func(context.Context, localhid.Request) (localhid.Reply, error)) (bool, error) {
	if ctx == nil || observe == nil || send == nil {
		return false, errNativeGuestState
	}
	current, err := observe()
	if err != nil {
		current = nativeGuestBinding{}
	}
	op, req, ok := a.nativeKeyboardTakeAction(expected, current)
	if !ok {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		a.nativeKeyboardCancelUnsent(op)
		return true, err
	}
	if req.Op != "reset" {
		before, beforeErr := observe()
		if beforeErr != nil || before != current || ctx.Err() != nil {
			if beforeErr == nil {
				beforeErr = errNativeGuestState
			}
			a.nativeKeyboardCancelUnsent(op)
			return true, beforeErr
		}
	}
	reply, sendErr := send(ctx, req)
	after, afterErr := observe()
	if afterErr != nil {
		after = nativeGuestBinding{}
	}
	a.nativeKeyboardCompleteAction(op, after, reply, sendErr)
	return true, errors.Join(sendErr, afterErr)
}

// nativeKeyboardCancelUnsent handles only an operation that the caller has
// proved was never dispatched. It cannot be used for a lost transport reply.
func (a *App) nativeKeyboardCancelUnsent(operation nativeGuestOperation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := &a.nativeGuest
	if s.operation != operation {
		return
	}
	switch operation.op {
	case "begin":
		generation, serial := s.lastGeneration, s.serial
		*s = nativeGuestLifecycle{lastGeneration: generation, serial: serial}
		a.nativeGuestSnapshotAllowed = false
	case "edge":
		s.operation = nativeGuestOperation{}
		s.pending = nil
		s.invalidate()
	case "reset":
		s.operation = nativeGuestOperation{}
		s.phase = nativeGuestNeedsReset
	default:
		return
	}
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
}

// nativeKeyboardTakeAction only extracts bookkeeping from the App. It starts
// no worker and performs no input I/O. current must be freshly derived from
// the current session, target, boot and lease observations by its caller.
func (a *App) nativeKeyboardTakeAction(expected, current nativeGuestBinding) (nativeGuestOperation, localhid.Request, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestBinding != expected {
		return nativeGuestOperation{}, localhid.Request{}, false
	}
	s := &a.nativeGuest
	if a.nativeGuestBeginRequest != nil {
		if !a.delegatedV2Active || s.phase != nativeGuestBeginning || s.invalidated || current != s.binding {
			// No BEGIN has been sent. Preserve the monotonic generation and
			// operation serial, and require a new snapshot before another one.
			generation, serial := s.lastGeneration, s.serial
			*s = nativeGuestLifecycle{lastGeneration: generation, serial: serial}
			a.nativeGuestBeginRequest = nil
			a.nativeGuestSnapshotAllowed = false
			return nativeGuestOperation{}, localhid.Request{}, false
		}
		req := *a.nativeGuestBeginRequest
		a.nativeGuestBeginRequest = nil
		return s.operation, req, true
	}
	if !a.delegatedV2Active {
		s.invalidate()
	}
	if s.phase == nativeGuestReady {
		if req, ok := s.nextEdge(current); ok {
			return s.operation, req, true
		}
	}
	if req, ok := s.reset(); ok {
		return s.operation, req, true
	}
	return nativeGuestOperation{}, localhid.Request{}, false
}

// nativeKeyboardCompleteAction accepts only the lifecycle's current one-shot
// cookie. A stale callback cannot acknowledge a newer source or operation.
func (a *App) nativeKeyboardCompleteAction(operation nativeGuestOperation, current nativeGuestBinding, reply localhid.Reply, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := &a.nativeGuest
	switch operation.op {
	case "begin":
		s.beginResult(operation, current, reply, err)
	case "edge":
		s.edgeResult(operation, current, reply, err)
	case "reset":
		s.resetResult(operation, reply, err)
	default:
		return
	}
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
}
