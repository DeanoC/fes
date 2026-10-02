package agent

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayRuntime interface {
	SetSessionDisplay(context.Context, bool, protocol.DevelopmentMediaBinding) *protocol.APIError
}

func (c *Coordinator) SetSessionDisplay(parent context.Context, visible bool, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
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
	ctx, cancel := context.WithTimeout(c.operationContext, 5*time.Second)
	defer cancel()
	// A display error never starts a recovery or replaces this active session.
	return c.Status(), runtime.SetSessionDisplay(ctx, visible, b)
}
