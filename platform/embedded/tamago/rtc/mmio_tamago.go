//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package rtc

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

// OpenMMIO maps an ARM PrimeCell PL031 RTC at a 4 KiB-aligned MMIO physical
// base address already mapped as device memory.
func OpenMMIO(base uint64) (*PL031, error) {
	if base == 0 || base&0xfff != 0 {
		return nil, fmt.Errorf("rtc: invalid PL031 base %#x", base)
	}
	return OpenPL031(mmioRegisters{base: base})
}

func read32(addr uint64) uint32
func write32(addr uint64, val uint32)
