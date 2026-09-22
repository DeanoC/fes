package agent

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaRuntime interface {
	ReplaceLiveMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) *protocol.APIError
	ClearLiveMedia(context.Context, protocol.DevelopmentMediaBinding) *protocol.APIError
}

func (c *Coordinator) ReplaceLiveMedia(parent context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return c.Status(), protocol.LiveMediaRequestError()
	}
	if !b.MatchesLive(c.Status()) {
		return c.Status(), protocol.LiveMediaIdentityError()
	}
	if !protocol.AdmitTapeMediaSize(size) {
		return c.Status(), protocol.LiveMediaRequestError()
	}
	data, err := protocol.ReadDevelopmentMedia(size, body)
	if err != nil {
		return c.Status(), protocol.LiveMediaRequestError()
	}
	if parent.Err() != nil {
		return c.Status(), &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "live media upload cancelled", Phase: "admission", Cause: parent.Err()}
	}
	runtime, ok := c.runtime.(liveMediaRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(c.operationContext, 15*time.Second)
	defer cancel()
	apiErr := runtime.ReplaceLiveMedia(ctx, int64(len(data)), bytes.NewReader(data), b)
	if apiErr != nil && apiErr.Phase != "request" && apiErr.Phase != "admission" && apiErr.Phase != "compatibility" && apiErr.Phase != "input" {
		observation, stop := context.WithTimeout(c.operationContext, c.stopTimeout)
		c.set(c.runtime.Reconcile(observation))
		stop()
	}
	return c.Status(), apiErr
}

func (c *Coordinator) ClearLiveMedia(parent context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return c.Status(), protocol.LiveMediaRequestError()
	}
	if !b.MatchesLive(c.Status()) {
		return c.Status(), protocol.LiveMediaIdentityError()
	}
	runtime, ok := c.runtime.(liveMediaRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(c.operationContext, 15*time.Second)
	defer cancel()
	apiErr := runtime.ClearLiveMedia(ctx, b)
	if apiErr != nil && apiErr.Phase != "request" && apiErr.Phase != "admission" && apiErr.Phase != "compatibility" && apiErr.Phase != "input" {
		observation, stop := context.WithTimeout(c.operationContext, c.stopTimeout)
		c.set(c.runtime.Reconcile(observation))
		stop()
	}
	return c.Status(), apiErr
}
