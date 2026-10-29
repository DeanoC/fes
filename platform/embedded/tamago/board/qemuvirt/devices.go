//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import (
	"fmt"
	"sync"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/usbarmory/tamago/dma"
)

// QEMU's interactive profile supplies 512 MiB RAM. Go owns the first 256 MiB;
// device buffers occupy a disjoint, non-executable, normal uncached range.
// These addresses describe QEMU virt only, never DE25 HPS DDR reservations.
const (
	DMABase      = 0x50000000
	DMABytes     = 64 << 20
	DisplayBase  = DMABase + DMABytes
	DisplayBytes = 8 << 20
)

var deviceOnce sync.Once
var displayMemory *dma.Region
var deviceError error

// InitDevices reserves the global disk/network DMA arena exactly once, then
// returns a separate framebuffer arena. No driver may reinitialize global DMA.
func InitDevices() (*dma.Region, error) {
	deviceOnce.Do(func() {
		deviceError = normalUncached(DMABase, 128<<20)
		if deviceError != nil {
			return
		}
		deviceError = dma.Init(DMABase, DMABytes)
		if deviceError == nil {
			displayMemory, deviceError = dma.NewRegion(DisplayBase, DisplayBytes, false)
		}
	})
	return displayMemory, deviceError
}

// This wrapper retains the QEMU-specific resource policy. The shared mapper
// never discovers or invents physical RAM/DMA reservations.
func normalUncached(base, size uint64) error {
	if base < 0x50000000 || size == 0 || base+size > 0x60000000 {
		return fmt.Errorf("invalid QEMU DMA mapping")
	}
	return arm64map.NormalUncached(base, size, 0x40000000, 256<<20)
}
