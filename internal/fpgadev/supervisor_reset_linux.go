//go:build linux && arm && fpgadev

package fpgadev

import (
	"context"
	"errors"
	"fmt"
)

// ResetSupervisor performs the fixed, bounded hardware reconciliation owned
// by the boot supervisor. It is intentionally not configurable: the Linux
// adapter opens the recovery mapping, applies the pinned bridge-disable tuple,
// verifies the manager control/status bits and independently observes all
// bridge views before returning. The mapping is closed even when the caller's
// context is cancelled.
func ResetSupervisor(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	recovery, err := NewMapper().openRecovery(ctx)
	if err != nil {
		return fmt.Errorf("open supervisor reset mapping: %w", err)
	}
	if recovery == nil {
		return errors.New("supervisor reset mapping is nil")
	}
	defer func() {
		closeErr := closeRecovery(context.Background(), recovery)
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close supervisor reset mapping: %w", closeErr))
		}
	}()
	if err := disableRecoveryBridges(ctx, recovery); err != nil {
		return fmt.Errorf("disable supervisor bridge tuple: %w", err)
	}
	readback, err := readRecoveryBridgeTuple(ctx, recovery)
	if err != nil {
		return fmt.Errorf("read supervisor bridge tuple: %w", err)
	}
	if readback.FPGAInterfaceModule != pinnedBridgeTuple.FPGAInterfaceModule ||
		readback.SDRPort != pinnedBridgeTuple.SDRPort ||
		readback.BridgeModuleReset != pinnedBridgeTuple.BridgeModuleReset ||
		readback.NIC301Remap != 0 {
		return errors.New("supervisor bridge reset tuple is not the pinned baseline")
	}
	views, err := newProductionBridgeStateObserver().VerifyBridgeViews(ctx)
	if err != nil {
		return fmt.Errorf("verify supervisor bridge views: %w", err)
	}
	bridge := BridgeProof{Requested: pinnedBridgeTuple, Readback: readback, L3RemapIssued: pinnedBridgeTuple.NIC301Remap, Views: views}
	if err := bridge.Validate(); err != nil {
		return fmt.Errorf("validate supervisor bridge reset proof: %w", err)
	}
	programming, err := readRecoveryProgramming(ctx, recovery)
	if err != nil {
		return fmt.Errorf("read supervisor programming state: %w", err)
	}
	if programming.Status&programmingUserModeMask != programmingUserModeValue {
		return errors.New("FPGA manager is not in USERMODE after supervisor reset")
	}
	if programming.Control&programmingForbiddenMask != 0 {
		return errors.New("FPGA manager control retains a programming bit after supervisor reset")
	}
	return contextError(ctx)
}
