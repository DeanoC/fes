package agent

import (
	"context"
	"io"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type partsCoreRuntime interface {
	LoadPartsCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader) (misterruntime.CoreActivation, bool, *protocol.APIError)
	InspectPartsCore(context.Context, int64, io.Reader) (protocol.PartsInspection, *protocol.APIError)
}
type libraryPartsCoreRuntime interface {
	LoadLibraryPartsCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader, string) (misterruntime.CoreActivation, bool, *protocol.APIError)
}

func (c *Coordinator) LoadPartsCore(ctx context.Context, size int64, body io.Reader) (protocol.Status, *protocol.APIError) {
	return c.loadCore(ctx, size, body, "", true, true)
}
func (c *Coordinator) LoadLibraryPartsCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, *protocol.APIError) {
	if id == "" {
		return c.Status(), &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "library parts require package identity", Phase: "admission"}
	}
	return c.loadCore(ctx, size, body, id, true, true)
}
func (c *Coordinator) InspectPartsCore(ctx context.Context, size int64, body io.Reader) (protocol.PartsInspection, *protocol.APIError) {
	if !c.begin() {
		return protocol.PartsInspection{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(partsCoreRuntime)
	if !ok {
		return protocol.PartsInspection{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	return runtime.InspectPartsCore(ctx, size, body)
}
