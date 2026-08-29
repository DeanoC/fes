//go:build !linux || (linux && !amd64 && !arm)

package fpgadev

import (
	"context"
	"errors"
)

// OpenMailbox is deliberately unavailable outside Linux.  Keeping the stub
// side-effect free lets callers use the same API while making the platform
// boundary explicit and testable.
func (m *Mapper) OpenMailbox() (Registers, error) {
	return nil, ErrUnsupported
}

// openRecovery is deliberately unavailable outside Linux. No descriptor or
// mapping is attempted on unsupported platforms.
func (m *Mapper) openRecovery(context.Context) (recoveryRegisters, error) {
	return nil, ErrUnsupported
}

type unsupportedBridgeStateObserver struct{}

func (unsupportedBridgeStateObserver) VerifyBridgeViews(context.Context) (BridgeViews, error) {
	return BridgeViews{}, errors.Join(ErrUnsupported, errors.New("Linux bridge-state observer unavailable"))
}

func newProductionBridgeStateObserver() BridgeVerifier {
	return unsupportedBridgeStateObserver{}
}
