//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
// Package board selects the boot hooks and peripherals at build time. The
// de25nano tag excludes the QEMU BSP; FAT configuration cannot switch hardware.
package board

import "image"

type Display interface{ Present(*image.RGBA) error }
type Network interface {
	Err() error
	Address() string
	Stats() string
}
