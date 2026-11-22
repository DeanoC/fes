// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

// PoolMemory adapts an uncached dmaops.Buffer allocated from a dmaops.Pool to
// the xgmac.Memory interface. Concurrent 16-byte XGMAC descriptors require
// Normal Non-Cacheable memory, so Cached pools are rejected.
type PoolMemory struct {
	buf       *dmaops.Buffer
	coherency dmaops.Coherency
	base      uint64
	size      uint64
}

var _ Memory = (*PoolMemory)(nil)

// NewPoolMemory allocates a 64-byte-aligned RequiredBytes DMA arena from pool.
// XGMAC's current 32-bit descriptor list setup requires the buffer to lie below 4 GiB.
func NewPoolMemory(pool *dmaops.Pool) (*PoolMemory, error) {
	if pool == nil {
		return nil, fmt.Errorf("xgmac: nil DMA pool")
	}
	if pool.Mode() != dmaops.Uncached {
		return nil, fmt.Errorf("xgmac: DMA pool must be uncached")
	}
	buf, err := pool.Alloc(RequiredBytes, 64)
	if err != nil {
		return nil, fmt.Errorf("xgmac: allocate DMA arena: %w", err)
	}
	base := buf.PhysAddr()
	size := buf.Len()
	if base >= 1<<32 || size > (1<<32)-base {
		_ = buf.Free()
		return nil, fmt.Errorf("xgmac: DMA arena [%#x, +%#x) exceeds 32-bit descriptor space", base, size)
	}
	return &PoolMemory{
		buf:       buf,
		coherency: pool.Coherency(),
		base:      base,
		size:      size,
	}, nil
}

func (m *PoolMemory) Base() uint64 { return m.base }
func (m *PoolMemory) Size() uint64 { return m.size }

func (m *PoolMemory) Read32(offset uint32) uint32 {
	v, err := m.buf.Read32(uint64(offset))
	if err != nil {
		return 0
	}
	return v
}

func (m *PoolMemory) Write32(offset, value uint32) {
	_ = m.buf.Write32(uint64(offset), value)
}

func (m *PoolMemory) Read(offset uint32, dst []byte) {
	if len(dst) == 0 {
		return
	}
	_, sub, err := m.buf.Slice(uint64(offset), uint64(len(dst)))
	if err != nil {
		return
	}
	copy(dst, sub)
}

func (m *PoolMemory) Write(offset uint32, src []byte) {
	if len(src) == 0 {
		return
	}
	_, sub, err := m.buf.Slice(uint64(offset), uint64(len(src)))
	if err != nil {
		return
	}
	copy(sub, src)
}

func (m *PoolMemory) Barrier() {
	m.coherency.SyncBarrier()
}

// Poison permanently pins the underlying dmaops.Buffer so it cannot be freed
// or reused after a fatal XGMAC fault or uncertain firmware boundary.
func (m *PoolMemory) Poison(err error) {
	m.buf.Poison(err)
}

// Buffer returns the underlying dmaops.Buffer for diagnostics and tests.
func (m *PoolMemory) Buffer() *dmaops.Buffer { return m.buf }

// Close frees the underlying buffer if it has not been poisoned.
func (m *PoolMemory) Close() error { return m.buf.Free() }
