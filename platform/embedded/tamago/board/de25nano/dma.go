//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import (
	"fmt"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

const maxNanoDMAPoolBytes = 256 << 20

// NewUncachedPool verifies that [base, base+size) is actively mapped as Normal
// Non-Cacheable memory in the live EL1 page tables (via arm64map.Verify) and
// wraps it in a dmaops.Pool backed by ARM64 hardware coherency barriers.
//
// This grants no physical RAM reservation or SMMU stream permission; callers
// must pass only an explicit, firmware-admitted DMA window.
func NewUncachedPool(base, size uint64) (*dmaops.Pool, error) {
	if base == 0 || size == 0 || size > maxNanoDMAPoolBytes || base+size < base {
		return nil, fmt.Errorf("de25-nano: invalid DMA pool range [%#x, +%#x)", base, size)
	}
	if err := InitDevices(); err != nil {
		return nil, err
	}
	if _, err := arm64map.Verify(profileRAMStart, profileRAMSize, []arm64map.Range{
		{Base: base, Bytes: size, Kind: arm64map.Uncached},
	}); err != nil {
		return nil, err
	}
	backing := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(base))), int(size))
	return dmaops.NewPool(base, backing, dmaops.Uncached, dmaops.Native())
}
