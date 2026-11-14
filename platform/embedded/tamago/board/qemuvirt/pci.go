//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// OpenUSBPCI assigns only the explicitly requested qemu-xhci at PCI 0:1.0.
// The runner selects virt,highmem=off, whose device tree puts ECAM at
// 0x3f000000 and the low PCI memory window at 0x10000000. Both are covered by
// TamaGo's existing Device mappings. This is not a Nano PCI or DWC2 driver.
func OpenUSBPCI() (uintptr, error) {
	const config = uintptr(0x3f000000 + 1<<15)
	const mmio = uint64(0x10000000)
	read := func(offset uintptr) uint32 {
		return atomic.LoadUint32((*uint32)(unsafe.Pointer(config + offset)))
	}
	write := func(offset uintptr, value uint32) {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(config+offset)), value)
	}
	if read(0) != 0x000d1b36 || read(8)>>8 != 0x0c0330 {
		return 0, fmt.Errorf("expected qemu-xhci at PCI 0:1.0 with low ECAM")
	}
	command := read(4) & 0xffff
	if command&6 != 0 {
		return 0, fmt.Errorf("QEMU xHCI memory/bus-master already enabled")
	}
	low, high := read(0x10), read(0x14)
	if low&7 != 4 {
		return 0, fmt.Errorf("QEMU xHCI requires a 64-bit memory BAR")
	}
	// Probe while decoding and bus mastering remain disabled, then restore the
	// original BAR before validating its mask. Failed admission cannot DMA.
	write(0x10, 0xffffffff)
	write(0x14, 0xffffffff)
	mask := uint64(read(0x14))<<32 | uint64(read(0x10)&^15)
	write(0x10, low)
	write(0x14, high)
	size := ^mask + 1
	if size < 4096 || size > 1<<20 || size&(size-1) != 0 || mmio&(size-1) != 0 {
		return 0, fmt.Errorf("unsupported QEMU xHCI BAR size %#x", size)
	}
	write(0x10, uint32(mmio)|low&15)
	write(0x14, uint32(mmio>>32))
	if read(0x10)&^15 != uint32(mmio) || read(0x14) != 0 {
		return 0, fmt.Errorf("QEMU xHCI BAR assignment did not latch")
	}
	write(4, command|6)
	if read(4)&6 != 6 {
		write(4, command)
		return 0, fmt.Errorf("QEMU xHCI memory/bus-master enable did not latch")
	}
	return uintptr(mmio), nil
}
