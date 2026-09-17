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

type ownedDevelopmentMediaRuntime interface {
	LoadDevelopmentMediaOwned(context.Context, context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) *protocol.APIError
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
	if !b.AcceptsSize(c.Status(), size) {
		return c.Status(), protocol.DevelopmentMediaRequestError()
	}
	var apiErr *protocol.APIError
	if !b.Stream {
		data, err := protocol.ReadDevelopmentMedia(size, body)
		if err != nil {
			return c.Status(), err
		}
		body = bytes.NewReader(data)
	}
	if parent.Err() != nil {
		return c.Status(), &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media upload cancelled", Phase: "admission", Cause: parent.Err()}
	}
	runtime, ok := c.runtime.(developmentMediaRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	if b.Stream {
		// The adapter stages with request/lease cancellation, then retains the
		// operation owner through the stream worker's bounded deadline.
		owned, ok := c.runtime.(ownedDevelopmentMediaRuntime)
		if !ok {
			return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
		}
		apiErr = owned.LoadDevelopmentMediaOwned(parent, c.operationContext, size, body, b)
	} else {
		ctx, cancel := context.WithTimeout(c.operationContext, 15*time.Second)
		defer cancel()
		apiErr = runtime.LoadDevelopmentMedia(ctx, size, body, b)
	}
	if apiErr != nil && apiErr.Phase != "request" && apiErr.Phase != "admission" && apiErr.Phase != "compatibility" {
		observation, stop := context.WithTimeout(c.operationContext, c.stopTimeout)
		c.set(c.runtime.Reconcile(observation))
		stop()
	}
	return c.Status(), apiErr
}
