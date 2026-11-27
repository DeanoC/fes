//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package i2c

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

// OpenMMIO maps a Synopsys DesignWare APB I2C controller at a 256-byte aligned
// MMIO physical base address already mapped as device memory.
func OpenMMIO(base uint64, cfg Config) (*Controller, error) {
	if base == 0 || base&0xff != 0 {
		return nil, fmt.Errorf("i2c: invalid controller base %#x", base)
	}
	return Open(mmioRegisters{base: base}, cfg)
}

func read32(addr uint64) uint32
func write32(addr uint64, val uint32)
