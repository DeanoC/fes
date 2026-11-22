//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import (
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/wdt"
)

// Agilex 5 HPS DesignWare APB Watchdog MMIO base addresses (socfpga_agilex5.dtsi).
const (
	L4WD0Base uint64 = 0x10d00200
	L4WD1Base uint64 = 0x10d00300
)

// OpenWatchdog probes Agilex 5 L4WD0 (index 0) or L4WD1 (index 1) without
// enabling the watchdog, returning the driver handle and its GIC-600 SPI INTID.
// Watchdog arming is strictly opt-in; callers must supply the active L4 clock
// frequency (AGILEX5_L4_SYS_FREE_CLK) and call Start explicitly.
func OpenWatchdog(index int, clockHz uint64) (*wdt.DW, uint32, error) {
	var base uint64
	var irq uint32
	switch index {
	case 0:
		base, irq = L4WD0Base, IRQWDT0
	case 1:
		base, irq = L4WD1Base, IRQWDT1
	default:
		return nil, 0, fmt.Errorf("de25nano: invalid watchdog index %d (want 0 or 1)", index)
	}
	if !releaseWatchdogReset(index, load32, store32) {
		return nil, 0, fmt.Errorf("de25nano: invalid watchdog index %d (want 0 or 1)", index)
	}
	dev, err := wdt.OpenMMIO(base, clockHz)
	if err != nil {
		return nil, 0, err
	}
	return dev, irq, nil
}
