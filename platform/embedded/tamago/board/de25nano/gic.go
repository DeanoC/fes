//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// Agilex 5 (GIC-600) base addresses and hardware interrupt IDs
// (from socfpga_agilex5.dtsi and Altera baremetal-drivers socfpga_interrupt.h).
const (
	GICDBase = 0x1d000000
	GICRBase = 0x1d060000

	IRQVirtTimer uint32 = 27 // PPI 11 (EL1 virtual timer)
	IRQPhysTimer uint32 = 30 // PPI 14 (EL1 non-secure physical timer)

	IRQFPGA2HPS0  uint32 = 48  // SPI 16 (first FPGA-to-HPS interrupt)
	IRQFPGA2HPS63 uint32 = 111 // SPI 79 (last FPGA-to-HPS interrupt)
	FPGAIRQCount  uint32 = 64  // 64 FPGA-to-HPS doorbell lines (0..63)

	IRQUSB0       uint32 = 125 // SPI 93  (DWC2 USB0)
	IRQSDMMC      uint32 = 128 // SPI 96  (Cadence SDHCI)
	IRQUART0      uint32 = 140 // SPI 108 (UART0)
	IRQUART1      uint32 = 141 // SPI 109 (UART1)
	IRQTimerL4SP0 uint32 = 145 // SPI 113 (DW APB timer 0)
	IRQTimerL4SP1 uint32 = 146 // SPI 114 (DW APB timer 1)
	IRQEMAC0      uint32 = 222 // SPI 190 (XGMAC 0)
	IRQEMAC1      uint32 = 239 // SPI 207 (XGMAC 1)
	IRQEMAC2      uint32 = 256 // SPI 224 (XGMAC 2)
)

var intc = gic.New(GICDBase, GICRBase)

// GIC returns the Agilex 5 GIC-600 controller. Call Init() before enabling IRQs.
func GIC() *gic.Controller { return intc }

// EMACIRQ maps an Agilex 5 XGMAC MMIO base address (MAC0/1/2) to its GIC-600 SPI INTID.
func EMACIRQ(base uintptr) (uint32, bool) {
	switch base {
	case 0x10810000:
		return IRQEMAC0, true
	case 0x10820000:
		return IRQEMAC1, true
	case 0x10830000:
		return IRQEMAC2, true
	default:
		return 0, false
	}
}

// FPGAIRQ maps an FPGA-to-HPS doorbell line (0..63) to its GIC-600 SPI INTID (48..111).
func FPGAIRQ(line uint32) (uint32, bool) {
	if line >= FPGAIRQCount {
		return 0, false
	}
	return IRQFPGA2HPS0 + line, true
}

// EnableFPGAIRQ configures an FPGA-to-HPS doorbell line (0..63) as edge-triggered
// (true) or level-sensitive (false), binds fn, clears any stale pending state,
// and unmasks the SPI in GIC-600. Call after GIC().Init().
func EnableFPGAIRQ(line uint32, edge bool, fn func()) error {
	id, ok := FPGAIRQ(line)
	if !ok {
		return errors.New("de25nano: FPGA IRQ line out of range")
	}
	if id >= intc.Lines() {
		return gic.ErrInvalidConfig
	}
	intc.Disable(id)
	intc.ClearPending(id)
	intc.SetEdge(id, edge)
	intc.Register(id, fn)
	intc.Enable(id)
	return nil
}

// DisableFPGAIRQ masks an FPGA-to-HPS doorbell line (0..63), clears any pending
// state, and unregisters its handler.
func DisableFPGAIRQ(line uint32) {
	id, ok := FPGAIRQ(line)
	if !ok {
		return
	}
	intc.Disable(id)
	intc.ClearPending(id)
	intc.Register(id, nil)
}
