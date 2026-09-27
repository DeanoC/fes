package agent

import (
	"context"
	"io"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaUnitRuntime interface {
	InsertMedia(context.Context, context.Context, int64, io.Reader, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError)
	EjectMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError)
	MediaUnits(context.Context, string, uint64) ([]protocol.MediaUnitStatus, bool)
}

// InsertMedia delivers one exact image into a declared unit of the active
// fes.computer generation while the machine runs. The request context bounds
// staging; the agent's operation context owns the single runtime call.
func (c *Coordinator) InsertMedia(parent context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if !b.Valid() || body == nil {
		return c.Status(), protocol.MediaUnitRequestError()
	}
	if !b.Matches(c.Status()) {
		return c.Status(), protocol.MediaUnitIdentityError()
	}
	if !b.AcceptsSize(c.Status(), size) {
		return c.Status(), protocol.MediaUnitRequestError()
	}
	runtime, ok := c.runtime.(mediaUnitRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	units, apiErr := runtime.InsertMedia(parent, c.operationContext, size, body, b)
	if apiErr != nil {
		c.refreshMediaUnits(runtime, b, apiErr)
		return c.Status(), apiErr
	}
	c.setMediaUnits(b, units)
	return c.Status(), nil
}

// EjectMedia empties one unit. Eject never replaces the running session.
func (c *Coordinator) EjectMedia(parent context.Context, b protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
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
	runtime, ok := c.runtime.(mediaUnitRuntime)
	if !ok {
		return c.Status(), &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(c.operationContext, c.stopTimeout)
	defer cancel()
	units, apiErr := runtime.EjectMedia(ctx, b)
	if apiErr != nil {
		c.refreshMediaUnits(runtime, b, apiErr)
		return c.Status(), apiErr
	}
	c.setMediaUnits(b, units)
	return c.Status(), nil
}

func (c *Coordinator) setMediaUnits(b protocol.MediaUnitBinding, units []protocol.MediaUnitStatus) {
	status := c.Status()
	if status.CorePackage == nil || status.CorePackage.PackageID != b.PackageID || status.CorePackage.Generation != b.Generation {
		return
	}
	status.CorePackage.MediaUnits = append([]protocol.MediaUnitStatus(nil), units...)
	c.set(status)
}

// refreshMediaUnits keeps the published unit states truthful after a failed
// transfer: the runtime leaves execution released and ejects the unit. A
// pre-dispatch rejection changed nothing; an unreadable result reconciles.
func (c *Coordinator) refreshMediaUnits(runtime mediaUnitRuntime, b protocol.MediaUnitBinding, apiErr *protocol.APIError) {
	switch apiErr.Phase {
	case "request", "admission":
		return
	}
	observation, cancel := context.WithTimeout(c.operationContext, c.stopTimeout)
	defer cancel()
	if units, ok := runtime.MediaUnits(observation, b.PackageID, b.Generation); ok {
		c.setMediaUnits(b, units)
		return
	}
	c.set(c.runtime.Reconcile(observation))
}
