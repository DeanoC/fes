//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import "github.com/DeanoC/fes/platform/embedded/tamago/gic"

// QEMU virt (gic-version=3) GICv3 base addresses and interrupt IDs.
const (
	GICDBase = 0x08000000
	GICRBase = 0x080a0000

	IRQVirtTimer uint32 = 27 // PPI 11 (EL1 virtual timer)
	IRQPhysTimer uint32 = 30 // PPI 14 (EL1 non-secure physical timer)
	IRQUART0     uint32 = 33 // SPI 1  (PL011 UART)
	IRQRTC       uint32 = 34 // SPI 2  (PL031 RTC)
)

var intc = gic.New(GICDBase, GICRBase)

// GIC returns the QEMU virt GICv3 controller. Call Init() before enabling IRQs.
func GIC() *gic.Controller { return intc }
