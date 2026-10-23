package agent

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type zx81TapeRuntime interface {
	CaptureZX81Tape(context.Context, string, uint64) ([]byte, *protocol.APIError)
	AcknowledgeZX81Tape(context.Context, string, uint64) *protocol.APIError
}

// CaptureZX81Tape reads the active machine's SAVE image. It does not change
// the cassette that is already loaded.
func (c *Coordinator) CaptureZX81Tape(parent context.Context, packageID string, generation uint64) ([]byte, *protocol.APIError) {
	if !c.begin() {
		return nil, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return nil, protocol.MediaUnitRequestError()
	}
	status := c.Status()
	if status.State != protocol.StateActive || status.CorePackage == nil || status.CorePackage.PackageID != packageID || status.CorePackage.Generation != generation || !protocol.Zx81TapeWriteCapable(status.CorePackage) {
		return nil, protocol.MediaUnitIdentityError()
	}
	runtime, ok := c.runtime.(zx81TapeRuntime)
	if !ok {
		return nil, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return runtime.CaptureZX81Tape(ctx, packageID, generation)
}

// AcknowledgeZX81Tape clears the FPGA SAVE snapshot after host persistence.
func (c *Coordinator) AcknowledgeZX81Tape(parent context.Context, packageID string, generation uint64) (protocol.Status, *protocol.APIError) {
	if !c.begin() {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another target transition is running"}
	}
	defer c.end()
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	status := c.Status()
	if status.State != protocol.StateActive || status.CorePackage == nil ||
		status.CorePackage.PackageID != packageID || status.CorePackage.Generation != generation ||
		!protocol.Zx81TapeWriteCapable(status.CorePackage) {
		return protocol.Status{}, protocol.MediaUnitIdentityError()
	}
	runtime, ok := c.runtime.(zx81TapeRuntime)
	if !ok {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if apiErr := runtime.AcknowledgeZX81Tape(ctx, packageID, generation); apiErr != nil {
		return protocol.Status{}, apiErr
	}
	return c.Status(), nil
}
