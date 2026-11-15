//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

var retainedWindowReloadPlan *nativefabric.StoredConfigurationPlan

// One explicit second load keeps the same boot owner, pinned frame arena and
// configuration deadline. It ends with closed bridges and contained SD.
func reloadWindow(v *nativefabric.HPSContextVerifier, control, configuration, contract []byte, previous *nativefabric.WindowSession, fresh windowStorageResult, limits scanout.ReaderLimits) error {
	returned, err := previous.Close()
	if err != nil {
		return err
	}
	plan, err := v.PreflightStoredConfiguration(control, configuration, fresh.selection)
	if err != nil {
		return err
	}
	retainedWindowReloadPlan = plan // Pin even a partially failed reservation until reboot.
	receipt := fresh.handoff.Receipt()
	fmt.Printf("FES_NATIVE_FPGA_WINDOW_RELOAD BEGIN package_id=%s owner=%x returned_generation=%d source_generation=%d cycles=2 memory_retained=1\n", v.Identity().PackageID, receipt.Owner, returned.After.Generation, receipt.Generation)
	var source nativefabric.HPSBootObservation
	retained, source, err = previous.NewStoredReconfigurationTransfer(plan, fresh.handoff, de25nano.ReserveConfiguration)
	if err != nil {
		return err
	}
	program(v, configuration, source)
	session, err := presentWindowFrames(v, control, contract, limits, 2)
	if err != nil {
		return err
	}
	closed, err := session.Close()
	if err != nil {
		return err
	}
	if closed.After.Owner != returned.After.Owner || closed.Source.Bridges.Generation != receipt.Generation ||
		closed.After.Generation != receipt.Generation+3 || closed.Frames != 2 || fresh.handoff.Receipt() != receipt {
		return errors.New("native repeated window lost its owned SD/configuration/return generations")
	}
	if _, err := fresh.handoff.Selection(); err != nil {
		return err
	}
	fmt.Printf("FES_NATIVE_FPGA_WINDOW_RELOAD PASS package_id=%s owner=%x source_generation=%d closed_generation=%d cycles=2 frames=2 data_closed=1 sd_contained=1 memory_retained=1 runtime_admission=unavailable\n", v.Identity().PackageID, closed.After.Owner, receipt.Generation, closed.After.Generation)
	return nil
}
