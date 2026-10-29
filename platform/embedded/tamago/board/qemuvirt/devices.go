//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

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

// normalUncached walks the architectural page tables installed by the pinned
// TamaGo runtime. It changes only previously unused 2 MiB identity-mapped
// blocks outside Go RAM. Device mappings forbid unaligned Go buffer accesses;
// normal uncached memory permits them while keeping DMA publication explicit.
func normalUncached(base, size uint64) error {
	const block = uint64(2 << 20)
	const addressMask = uint64(0x0000fffffffff000)
	if base%block != 0 || size == 0 || size%block != 0 || base < 0x50000000 || base+size > 0x60000000 {
		return fmt.Errorf("invalid QEMU DMA mapping")
	}
	root := tableBase() & addressMask
	if root < 0x40000000 || root >= 0x50000000 {
		return fmt.Errorf("page-table root lies outside Go RAM")
	}
	var entries []uintptr
	for addr := base; addr < base+size; addr += block {
		l1 := atomic.LoadUint64((*uint64)(unsafe.Pointer(uintptr(root + ((addr>>30)&511)*8))))
		if l1&3 != 3 {
			return fmt.Errorf("QEMU DMA range lacks an L2 table")
		}
		table := l1 & addressMask
		if table < 0x40000000 || table >= 0x50000000 {
			return fmt.Errorf("QEMU DMA L2 table lies outside Go RAM")
		}
		entry := uintptr(table + ((addr>>21)&511)*8)
		descriptor := atomic.LoadUint64((*uint64)(unsafe.Pointer(entry)))
		if descriptor&3 != 1 || descriptor&0x0000ffffffe00000 != addr {
			return fmt.Errorf("QEMU DMA range is not identity mapped")
		}
		entries = append(entries, entry)
	}
	setUncachedAttribute()
	for _, entry := range entries {
		p := (*uint64)(unsafe.Pointer(entry))
		old := atomic.LoadUint64(p)
		// Break-before-make, cleaning the page-table entry before each TLBI.
		atomic.StoreUint64(p, 0)
		publishTableEntry(entry)
		// AttrIndx=2, outer-shareable, AF, RW at EL1, PXN/UXN, block.
		updated := (old &^ (uint64(7)<<2 | uint64(3)<<8)) | 2<<2 | 2<<8 | 1<<10 | 3<<53
		atomic.StoreUint64(p, updated)
		publishTableEntry(entry)
	}
	return nil
}

func tableBase() uint64
func setUncachedAttribute()
func publishTableEntry(uintptr)
