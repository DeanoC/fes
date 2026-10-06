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
	prior := c.Status()
	if !b.MatchesForSave(prior) {
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
	if !c.publishMediaSave(b, prior, units) {
		return c.Status(), protocol.MediaUnitIdentityError()
	}
	return c.Status(), nil
}

// publishMediaSave clears a retained save error only after a successful runtime
// call confirms the same durable disk. Status publication and its identity
// check share the lock so a later fault cannot be overwritten.
func (c *Coordinator) publishMediaSave(b protocol.MediaUnitBinding, prior protocol.Status, units []protocol.MediaUnitStatus) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !b.MatchesForSave(c.status) {
		return false
	}
	after := cloneStatus(c.status)
	after.CorePackage.MediaUnits = protocol.CloneMediaUnits(units)
	after.LastError = nil
	if !b.MatchesSaveResult(prior, after) || !b.MatchesSaveResult(c.status, after) {
		return false
	}
	c.status = after
	return true
}
