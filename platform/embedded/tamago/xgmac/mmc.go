// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// Synopsys DesignWare XGMAC 2.10 MAC Management Counter (MMC) register offsets
// matching Linux stmmac mmc_core.c relative to the MAC MMIO base (0x800).
const (
	mmcControl         = 0x800
	mmcRXIntMask       = 0x80c
	mmcTXIntMask       = 0x810
	mmcTXOctetsGB      = 0x814 // 64-bit lo/hi pair
	mmcTXPacketsGB     = 0x81c // 64-bit lo/hi pair
	mmcRXPacketsGB     = 0x900 // 64-bit lo/hi pair
	mmcRXOctetsGB      = 0x908 // 64-bit lo/hi pair
	mmcRXCRCErrors     = 0x928 // 64-bit lo/hi pair
	mmcRXRuntErrors    = 0x930 // 32-bit
	mmcRXJabberErrors  = 0x934 // 32-bit
	mmcRXUndersizeGood = 0x938 // 32-bit
	mmcRXLengthErrors  = 0x978 // 64-bit lo/hi pair
	mmcRXFIFOOverflows = 0x990 // 64-bit lo/hi pair

	mmcCtrlCounterReset = uint32(1 << 0)
	mmcCtrlResetOnRead  = uint32(1 << 2)
)

// HardwareStats reports hardware MAC Management Counters read from the XGMAC
// MMC block. When Config.ResetMMCOnRead is true, HardwareStats accumulates
// clear-on-read deltas across polls and BindControl.Stop/Start cycles.
type HardwareStats struct {
	TXOctetsGB      uint64
	TXPacketsGB     uint64
	RXPacketsGB     uint64
	RXOctetsGB      uint64
	RXCRCErrors     uint64
	RXRuntErrors    uint64
	RXJabberErrors  uint64
	RXUndersizeGood uint64
	RXLengthErrors  uint64
	RXFIFOOverflows uint64
}

func (d *Device) initMMCLocked() {
	d.io.Write32(mmcRXIntMask, 0xffffffff)
	d.io.Write32(mmcTXIntMask, 0xffffffff)
	d.io.Write32(mmcControl, mmcCtrlResetOnRead|mmcCtrlCounterReset)
}

func (d *Device) readMMC64Locked(loReg uint32) uint64 {
	lo := uint64(d.io.Read32(loReg))
	hi := uint64(d.io.Read32(loReg + 4))
	return (hi << 32) | lo
}

func (d *Device) readMMCOnceLocked() HardwareStats {
	return HardwareStats{
		TXOctetsGB:      d.readMMC64Locked(mmcTXOctetsGB),
		TXPacketsGB:     d.readMMC64Locked(mmcTXPacketsGB),
		RXPacketsGB:     d.readMMC64Locked(mmcRXPacketsGB),
		RXOctetsGB:      d.readMMC64Locked(mmcRXOctetsGB),
		RXCRCErrors:     d.readMMC64Locked(mmcRXCRCErrors),
		RXRuntErrors:    uint64(d.io.Read32(mmcRXRuntErrors)),
		RXJabberErrors:  uint64(d.io.Read32(mmcRXJabberErrors)),
		RXUndersizeGood: uint64(d.io.Read32(mmcRXUndersizeGood)),
		RXLengthErrors:  d.readMMC64Locked(mmcRXLengthErrors),
		RXFIFOOverflows: d.readMMC64Locked(mmcRXFIFOOverflows),
	}
}

func (d *Device) accumulateMMCLocked() HardwareStats {
	delta := d.readMMCOnceLocked()
	d.hwStats.TXOctetsGB += delta.TXOctetsGB
	d.hwStats.TXPacketsGB += delta.TXPacketsGB
	d.hwStats.RXPacketsGB += delta.RXPacketsGB
	d.hwStats.RXOctetsGB += delta.RXOctetsGB
	d.hwStats.RXCRCErrors += delta.RXCRCErrors
	d.hwStats.RXRuntErrors += delta.RXRuntErrors
	d.hwStats.RXJabberErrors += delta.RXJabberErrors
	d.hwStats.RXUndersizeGood += delta.RXUndersizeGood
	d.hwStats.RXLengthErrors += delta.RXLengthErrors
	d.hwStats.RXFIFOOverflows += delta.RXFIFOOverflows
	return d.hwStats
}

// HardwareStats returns XGMAC MMC hardware transmit, receive, and error counters.
// Returns network.ErrPaused while contained via BindControl.Stop.
func (d *Device) HardwareStats() (HardwareStats, error) {
	if d == nil {
		return HardwareStats{}, network.ErrControl
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.Err(); err != nil {
		return HardwareStats{}, err
	}
	if d.paused {
		return HardwareStats{}, network.ErrPaused
	}
	if d.config.ResetMMCOnRead {
		return d.accumulateMMCLocked(), nil
	}
	return d.readMMCOnceLocked(), nil
}
