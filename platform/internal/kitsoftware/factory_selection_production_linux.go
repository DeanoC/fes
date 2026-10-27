package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// SelectFactory is a root-only local journal action for a kit whose supervisor
// refused spent recovery. It never starts, stops or signals a service, calls
// admission or changes the image. The waiting supervisor starts the selection.
func SelectFactory(ctx context.Context, req AcknowledgeRequest) (FactorySelectionResult, error) {
	var result FactorySelectionResult
	if os.Geteuid() != 0 {
		return result, errors.New("factory selection requires root")
	}
	if err := req.validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	repair, err := openOperatorRepair(ctx, req)
	if err != nil {
		return result, err
	}
	defer repair.repo.Close()
	// Verify the factory as the supervisor will start it. A later image resolves
	// this selection by content ID from the copy the supervisor retains.
	if _, err := repair.resolve(ctx, repair.factory.ID); err != nil {
		return result, err
	}
	if _, err := repair.store.Read(ctx, repair.factory.ID); err != nil {
		return result, fmt.Errorf("factory software is not retained: %w", err)
	}
	return selectFactory(ctx, repair.repo, req, repair.factory.ID, func(ctx context.Context) error {
		if err := acknowledgmentMachine(ctx, req); err != nil {
			return err
		}
		return exhaustedIdle(maintenanceMarkerPath, req.Boot, CheckNoManagedProcesses)
	})
}

// exhaustedIdle is read-only. A running group belongs to acknowledge or
// prepare-power-cycle. A live reservation from this boot would keep the next
// boot admission waiting behind the failed operation, so it needs the hard
// power-supply cycle first. Boot clears the marker; the agent retires a marker
// from another boot and has nothing to do for a completed one.
func exhaustedIdle(markerPath, boot string, noneRunning func() error) error {
	if err := noneRunning(); err != nil {
		return fmt.Errorf("supervised software is running; use acknowledge or prepare-power-cycle: %w", err)
	}
	var marker powerCycleMarker
	err := readPrivatePowerCycleJSON(markerPath, &marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if marker.BootID == boot && marker.State != "completed" {
		return fmt.Errorf("this boot still holds the %s maintenance reservation of operation %s; perform the hard power-supply cycle first", marker.State, marker.OperationID)
	}
	return nil
}
