package agent

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type developmentMediaRuntime interface {
	LoadDevelopmentMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) *protocol.APIError
}

func (c *Coordinator) LoadDevelopmentMedia(parent context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() {
		return c.Status(), protocol.DevelopmentMediaRequestError()
	}
	if !b.Matches(c.Status()) {
		return c.Status(), protocol.DevelopmentMediaIdentityError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(size, body)
	if apiErr != nil {
		return c.Status(), apiErr
	}
	if parent.Err() != nil {
		return c.Status(), &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media upload cancelled", Phase: "admission"}
	}
	runtime, ok := c.runtime.(developmentMediaRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	// Upload admission is cancellable by the request/lease. Once dispatched,
	// retain the existing operation owner through the runtime's 10s deadline.
	ctx, cancel := context.WithTimeout(c.operationContext, 15*time.Second)
	defer cancel()
	apiErr = runtime.LoadDevelopmentMedia(ctx, size, bytes.NewReader(data), b)
	if apiErr != nil && apiErr.Phase != "request" && apiErr.Phase != "admission" && apiErr.Phase != "compatibility" {
		observation, stop := context.WithTimeout(c.operationContext, c.stopTimeout)
		c.set(c.runtime.Reconcile(observation))
		stop()
	}
	return c.Status(), apiErr
}
