package agent

import (
	"context"
	"io"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type libraryCoreRuntime interface {
	LoadLibraryCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader, string) (misterruntime.CoreActivation, bool, *protocol.APIError)
}
type coreDataRuntime interface {
	InspectCoreData(context.Context, int64, io.Reader, string) (protocol.CoreDataInspection, *protocol.APIError)
	UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError)
}

func (c *Coordinator) LoadLibraryCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, *protocol.APIError) {
	return c.loadCore(ctx, size, body, id)
}
func (c *Coordinator) InspectCoreData(ctx context.Context, size int64, body io.Reader, id string) (protocol.CoreDataInspection, *protocol.APIError) {
	return c.coreData(ctx, size, body, id, nil)
}
func (c *Coordinator) UpdateCoreSettings(ctx context.Context, size int64, body io.Reader, u protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	return c.coreData(ctx, size, body, u.ExpectedPackageID, &u)
}
func (c *Coordinator) coreData(ctx context.Context, size int64, body io.Reader, id string, u *protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	if !c.begin() {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(coreDataRuntime)
	if !ok {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	if u == nil {
		return runtime.InspectCoreData(ctx, size, body, id)
	}
	status := c.Status()
	if status.State == protocol.StateFailed || status.Recovery != "" {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "target recovery must finish before changing settings"}
	}
	return runtime.UpdateCoreSettings(ctx, size, body, *u)
}

type composedCoreRuntime interface {
	LoadComposedCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader, string) (misterruntime.CoreActivation, bool, *protocol.APIError)
}

func (c *Coordinator) LoadComposedCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, *protocol.APIError) {
	return c.loadCore(ctx, size, body, id, true)
}
