//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
// Package board selects the boot hooks and peripherals at build time. The
// de25nano tag excludes the QEMU BSP; FAT configuration cannot switch hardware.
package board

import "image"

// Architectural ARMv8 Generic Timer PPI interrupt IDs shared by all boards.
const (
	IRQVirtTimer uint32 = 27 // PPI 11 (EL1 virtual timer)
	IRQPhysTimer uint32 = 30 // PPI 14 (EL1 non-secure physical timer)
)

type Display interface{ Present(*image.RGBA) error }
type Network interface {
	Err() error
	Address() string
	Stats() string
}
