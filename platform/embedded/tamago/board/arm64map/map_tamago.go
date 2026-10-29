//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// NormalUncached walks the architectural page tables installed by the pinned
// TamaGo runtime. It changes only previously unused 2 MiB identity-mapped
// blocks outside Go RAM. Device mappings forbid unaligned Go buffer accesses;
// normal uncached memory permits them while keeping DMA publication explicit.
func NormalUncached(base, size, ramBase, ramSize uint64) error {
	const block = uint64(2 << 20)
	const addressMask = uint64(0x0000fffffffff000)
	if err := Validate(base, size, ramBase, ramSize); err != nil {
		return err
	}
	root := tableBase() & addressMask
	if root < ramBase || root+4096 > ramBase+ramSize {
		return fmt.Errorf("page-table root lies outside Go RAM")
	}
	var entries []uintptr
	for addr := base; addr < base+size; addr += block {
		l1 := atomic.LoadUint64((*uint64)(unsafe.Pointer(uintptr(root + ((addr>>30)&511)*8))))
		if l1&3 != 3 {
			return fmt.Errorf("DMA range lacks an L2 table")
		}
		table := l1 & addressMask
		if table < ramBase || table+4096 > ramBase+ramSize {
			return fmt.Errorf("DMA L2 table lies outside Go RAM")
		}
		entry := uintptr(table + ((addr>>21)&511)*8)
		descriptor := atomic.LoadUint64((*uint64)(unsafe.Pointer(entry)))
		if descriptor&3 != 1 || descriptor&0x0000ffffffe00000 != addr {
			return fmt.Errorf("DMA range is not identity mapped")
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
