// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

// PoolMemory adapts an uncached dmaops.Buffer allocated from a dmaops.Pool to
// the xgmac.Memory interface. Concurrent 16-byte XGMAC descriptors require
// Normal Non-Cacheable memory, so Cached pools are rejected.
//
// Matching xgmac.Memory's ownership contract, PoolMemory exposes no free
// operation and can be adopted by at most one Open call: once allocated for an
// XGMAC instance, its DMA span remains permanently pinned until reboot.
type PoolMemory struct {
	buf       *dmaops.Buffer
	coherency dmaops.Coherency
	base      uint64
	size      uint64
	adopted   atomic.Bool
}

var _ Memory = (*PoolMemory)(nil)

// NewPoolMemory allocates a page-aligned RequiredBytes DMA arena from pool.
// XGMAC's current 32-bit descriptor list setup requires the buffer to lie below 4 GiB.
func NewPoolMemory(pool *dmaops.Pool) (*PoolMemory, error) {
	if pool == nil {
		return nil, fmt.Errorf("xgmac: nil DMA pool")
	}
	if pool.Mode() != dmaops.Uncached {
		return nil, fmt.Errorf("xgmac: DMA pool must be uncached")
	}
	buf, err := pool.Alloc(RequiredBytes, dmaPageSize)
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

func (m *PoolMemory) adopt() error {
	if !m.adopted.CompareAndSwap(false, true) {
		return errors.New("xgmac: DMA memory already adopted by another device")
	}
	return nil
}

func (m *PoolMemory) Base() uint64 { return m.base }
func (m *PoolMemory) Size() uint64 { return m.size }

// Cap returns the reserved span size in the underlying dmaops.Pool.
func (m *PoolMemory) Cap() uint64 { return m.buf.Cap() }

// Err returns the poison error recorded on the underlying dmaops.Buffer, if any.
func (m *PoolMemory) Err() error { return m.buf.Err() }

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

// Poison permanently marks the underlying dmaops.Buffer poisoned after a fatal
// XGMAC fault or uncertain firmware boundary.
func (m *PoolMemory) Poison(err error) {
	m.buf.Poison(err)
}
