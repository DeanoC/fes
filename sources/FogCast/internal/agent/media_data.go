package agent

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"time"
)

type mediaDataRuntime interface {
	mediaUnitRuntime
	InsertLibraryMedia(context.Context, context.Context, int64, io.Reader, protocol.LibraryMediaBinding) ([]protocol.MediaUnitStatus, *protocol.APIError)
	SaveMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError)
}

func (c *Coordinator) InsertLibraryMedia(parent context.Context, size int64, body io.Reader, b protocol.LibraryMediaBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() || body == nil || size != protocol.AtariStFloppyBytes {
		return c.Status(), protocol.MediaUnitRequestError()
	}
	if !b.MediaUnitBinding.Matches(c.Status()) {
		return c.Status(), protocol.MediaUnitIdentityError()
	}
	runtime, ok := c.runtime.(mediaDataRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	units, err := runtime.InsertLibraryMedia(parent, c.operationContext, size, body, b)
	if err != nil {
		c.refreshMediaUnits(runtime, b.MediaUnitBinding, err)
		return c.Status(), err
	}
	c.setMediaUnits(b.MediaUnitBinding, units)
	return c.Status(), nil
}
func (c *Coordinator) SaveMedia(parent context.Context, b protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() {
		return c.Status(), protocol.MediaUnitRequestError()
	}
	if !b.Matches(c.Status()) {
		return c.Status(), protocol.MediaUnitIdentityError()
	}
	runtime, ok := c.runtime.(mediaDataRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(c.operationContext, 135*time.Second)
	defer cancel()
	units, err := runtime.SaveMedia(ctx, b)
	if err != nil {
		c.refreshMediaUnits(runtime, b, err)
		return c.Status(), err
	}
	c.setMediaUnits(b, units)
	return c.Status(), nil
}
