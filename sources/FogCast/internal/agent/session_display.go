package agent

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayRuntime interface {
	SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) *protocol.APIError
}

func (c *Coordinator) SetSessionDisplay(parent context.Context, display protocol.SessionDisplay, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.MatchesSessionDisplay(c.Status()) {
		return c.Status(), protocol.SessionDisplayIdentityError()
	}
	if parent.Err() != nil {
		return c.Status(), &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "launcher display request cancelled", Phase: "admission", Cause: parent.Err()}
	}
	runtime, ok := c.runtime.(sessionDisplayRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}

	// Every request, including one initiated by the launcher, crosses the
	// same fence. The host protocol therefore remains unchanged.
	var prepared *sessiondisplayipc.Prepared
	if c.launcherDisplaySocket != "" {
		transition := sessiondisplayipc.Transition{PackageID: b.PackageID, Generation: b.Generation, TargetID: b.TargetID, Display: display}
		guard, err := sessiondisplayipc.AcquireDisplayGuard(parent, c.launcherDisplaySocket)
		if err != nil {
			return c.Status(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher display coordination unavailable", Phase: "admission", Cause: err}
		}
		defer func() {
			if guard != nil {
				_ = guard.Release()
			}
		}()
		if c.launcherDisplayTargetID == "" || b.TargetID != c.launcherDisplayTargetID {
			// While holding the publication guard, a missing socket really is
			// headless. Any existing or ambiguous path fails before invoking
			// the launcher's input/presentation fence.
			if _, statErr := os.Lstat(c.launcherDisplaySocket); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
				return c.Status(), protocol.SessionDisplayIdentityError()
			}
		} else {
			var present bool
			prepared, present, err = sessiondisplayipc.Prepare(parent, c.launcherDisplaySocket, transition)
			if err != nil {
				return c.Status(), &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher could not prepare the display", Phase: "admission", Cause: err}
			}
			if present {
				_ = guard.Release()
				guard = nil
			}
		}
	}
	if parent.Err() != nil || !b.MatchesSessionDisplay(c.Status()) {
		if prepared != nil {
			finishCtx, finishCancel := context.WithTimeout(c.operationContext, 5*time.Second)
			_ = prepared.Finish(finishCtx, sessiondisplayipc.Rejected)
			finishCancel()
		}
		return c.Status(), protocol.SessionDisplayIdentityError()
	}
	ctx, cancel := context.WithTimeout(c.operationContext, 5*time.Second)
	defer cancel()
	// A display error never starts a recovery or replaces this active session.
	apiErr := runtime.SetSessionDisplay(ctx, display, b)
	if prepared != nil {
		outcome := sessiondisplayipc.Applied
		if apiErr != nil {
			outcome = sessiondisplayipc.Unknown
		}
		finishCtx, finishCancel := context.WithTimeout(c.operationContext, 5*time.Second)
		err := prepared.Finish(finishCtx, outcome)
		finishCancel()
		if err != nil && apiErr == nil {
			apiErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher display changed but presentation is unconfirmed", Phase: "display", Cause: err}
		}
	}
	return c.Status(), apiErr
}
