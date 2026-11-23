//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import "fmt"

type mmioRegisters struct {
	base uint64
}

func (m mmioRegisters) Read32(offset uint32) uint32 {
	return read32(m.base + uint64(offset))
}

func (m mmioRegisters) Write32(offset uint32, val uint32) {
	write32(m.base+uint64(offset), val)
}

// OpenMMIO probes a DesignWare APB Watchdog at a 256-byte-aligned MMIO base
// address (such as Agilex 5 L4WD0 0x10d00200 or L4WD1 0x10d00300) without
// enabling the watchdog.
func OpenMMIO(base uint64, clockHz uint64) (*DW, error) {
	if base == 0 || base&0xff != 0 {
		return nil, fmt.Errorf("%w: unaligned WDT base %#x", ErrInvalidConfig, base)
	}
	return Open(mmioRegisters{base: base}, clockHz)
}

func read32(addr uint64) uint32
func write32(addr uint64, val uint32)
