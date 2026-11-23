// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

const (
	agilex5RSTMGRBase       uintptr = 0x10d11000
	agilex5PER1MODRSTOffset uintptr = 0x28
	// WATCHDOG0_RESET and WATCHDOG1_RESET are IDs 64 and 65, bits 0 and 1.
	agilex5L4WD0ResetMask uint32 = 1 << 0
	agilex5L4WD1ResetMask uint32 = 1 << 1
)

// releaseWatchdogReset deasserts only the selected L4 watchdog reset in the
// Agilex 5 PER1MODRST register. It leaves watchdog enable under explicit caller
// control; probing the DesignWare block does not set WDT_EN.
func releaseWatchdogReset(index int, read32 func(uintptr) uint32, write32 func(uintptr, uint32)) bool {
	var mask uint32
	switch index {
	case 0:
		mask = agilex5L4WD0ResetMask
	case 1:
		mask = agilex5L4WD1ResetMask
	default:
		return false
	}
	if read32 == nil || write32 == nil {
		return false
	}

	addr := agilex5RSTMGRBase + agilex5PER1MODRSTOffset
	write32(addr, read32(addr)&^mask)
	return true
}
