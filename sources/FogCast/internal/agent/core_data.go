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

type machineCoreRuntime interface {
	LoadLibraryMachineCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader, string, protocol.LibraryMachineBinding) (misterruntime.CoreActivation, bool, *protocol.APIError)
	InspectGameCoreData(context.Context, int64, io.Reader, string, string) (protocol.CoreDataInspection, *protocol.APIError)
}
type diskImageRuntime interface {
	DiskImage(context.Context, string) (protocol.DiskImageStatus, *protocol.APIError)
	StageDiskImage(context.Context, string, int64, io.Reader) (protocol.DiskImageStatus, *protocol.APIError)
}

// LoadLibraryMachineCore launches a pc386 library game with its staged disk.
func (c *Coordinator) LoadLibraryMachineCore(ctx context.Context, size int64, body io.Reader, id string, machine protocol.LibraryMachineBinding) (protocol.Status, *protocol.APIError) {
	if !protocol.ValidMediaGameID(machine.GameID) {
		return c.Status(), &protocol.APIError{Code: protocol.CodeBadRequest, Message: "library game is invalid", Phase: "request"}
	}
	return c.loadBoundCore(ctx, size, body, id, &machine)
}

// InspectGameCoreData reads a pc386 library game's machine record.
func (c *Coordinator) InspectGameCoreData(ctx context.Context, size int64, body io.Reader, id, gameID string) (protocol.CoreDataInspection, *protocol.APIError) {
	if !c.begin() {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(machineCoreRuntime)
	if !ok {
		return protocol.CoreDataInspection{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	return runtime.InspectGameCoreData(ctx, size, body, id, gameID)
}

// DiskImage reports a staged hard-disk image without changing the kit.
func (c *Coordinator) DiskImage(ctx context.Context, sha string) (protocol.DiskImageStatus, *protocol.APIError) {
	runtime, ok := c.runtime.(diskImageRuntime)
	if !ok {
		return protocol.DiskImageStatus{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	return runtime.DiskImage(ctx, sha)
}

// StageDiskImage writes a hard-disk image; it runs as a target transition so
// it never overlaps a launch reading the staged images.
func (c *Coordinator) StageDiskImage(ctx context.Context, sha string, size int64, body io.Reader) (protocol.DiskImageStatus, *protocol.APIError) {
	if !c.begin() {
		return protocol.DiskImageStatus{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	runtime, ok := c.runtime.(diskImageRuntime)
	if !ok {
		return protocol.DiskImageStatus{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	return runtime.StageDiskImage(ctx, sha, size, body)
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
