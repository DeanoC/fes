// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import (
	"encoding/binary"
	"fmt"
	"sync"
)

type freeSpan struct {
	offset uint64
	size   uint64
}

// Stats summarizes the current occupancy and fault state of a Pool.
type Stats struct {
	Base            uint64
	Size            uint64
	Mode            MemoryMode
	CacheLine       uint64
	AllocatedBytes  uint64
	FreeBytes       uint64
	PinnedBytes     uint64
	LiveBuffers     int
	PoisonedBuffers int
	FreeSpans       int
	Poisoned        bool
}

// Pool manages aligned DMA allocations over an explicit, pre-mapped memory
// arena outside the Go heap (or a caller-provided backing slice in host tests).
//
// Poisoned buffers and poisoned pools fail closed: their physical spans remain
// permanently pinned and are never returned to the free list.
type Pool struct {
	mu              sync.Mutex
	base            uint64
	backing         []byte
	mode            MemoryMode
	hw              Coherency
	cacheLine       uint64
	minAlign        uint64
	free            []freeSpan
	allocatedBytes  uint64
	pinnedBytes     uint64
	liveBuffers     int
	poisonedBuffers int
	poisonErr       error
}

// NewPool creates a DMA Pool over [base, base+len(backing)).
//
// For Cached pools, both base and len(backing) must be multiples of
// hw.CacheLineSize() so no allocated buffer can share a cache line with memory
// outside the pool. For Uncached pools, base must be at least 8-byte aligned.
func NewPool(base uint64, backing []byte, mode MemoryMode, hw Coherency) (*Pool, error) {
	if hw == nil || len(backing) == 0 || (mode != Uncached && mode != Cached) {
		return nil, ErrInvalidConfig
	}
	line := hw.CacheLineSize()
	if !validCacheLine(line) {
		return nil, fmt.Errorf("%w: unsupported cache line size %d", ErrInvalidConfig, line)
	}
	size := uint64(len(backing))
	if base+size < base {
		return nil, fmt.Errorf("%w: address space overflow", ErrInvalidConfig)
	}
	minAlign := uint64(minAlignUncached)
	if mode == Cached {
		minAlign = line
		if base%line != 0 || size%line != 0 {
			return nil, fmt.Errorf("%w: cached pool base and size must align to cache line (%d)", ErrInvalidConfig, line)
		}
	} else if base%minAlign != 0 {
		return nil, fmt.Errorf("%w: uncached pool base must be %d-byte aligned", ErrInvalidConfig, minAlign)
	}
	return &Pool{
		base:      base,
		backing:   backing,
		mode:      mode,
		hw:        hw,
		cacheLine: line,
		minAlign:  minAlign,
		free:      []freeSpan{{offset: 0, size: size}},
	}, nil
}

// Base returns the arena's physical/bus base address.
func (p *Pool) Base() uint64 { return p.base }

// Size returns the arena's total size in bytes.
func (p *Pool) Size() uint64 { return uint64(len(p.backing)) }

// Mode returns the pool's memory attribute mode (Uncached or Cached).
func (p *Pool) Mode() MemoryMode { return p.mode }

// Coherency returns the pool's coherency provider.
func (p *Pool) Coherency() Coherency { return p.hw }

// Poison permanently poisons the pool. Future Alloc and Free calls fail with
// ErrPoisoned, and all existing buffers remain pinned until reboot.
func (p *Pool) Poison(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisonErr == nil {
		if err == nil {
			err = ErrPoisoned
		}
		p.poisonErr = err
	}
}

// Err returns the pool's poison error, or nil if healthy.
func (p *Pool) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.poisonErr
}

// Stats returns a point-in-time snapshot of pool usage and poison state.
func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	var freeBytes uint64
	for _, s := range p.free {
		freeBytes += s.size
	}
	return Stats{
		Base:            p.base,
		Size:            uint64(len(p.backing)),
		Mode:            p.mode,
		CacheLine:       p.cacheLine,
		AllocatedBytes:  p.allocatedBytes,
		FreeBytes:       freeBytes,
		PinnedBytes:     p.pinnedBytes,
		LiveBuffers:     p.liveBuffers,
		PoisonedBuffers: p.poisonedBuffers,
		FreeSpans:       len(p.free),
		Poisoned:        p.poisonErr != nil,
	}
}

// Alloc reserves a zeroed DMA buffer of at least size bytes aligned to align
// (or the pool's minimum alignment, whichever is larger). In Cached mode, both
// start address and reserved span are aligned to CacheLineSize so no two
// buffers share a cache line.
func (p *Pool) Alloc(size, align uint64) (*Buffer, error) {
	if size == 0 || (align != 0 && !isPowerOfTwo(align)) {
		return nil, ErrInvalidConfig
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisonErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrPoisoned, p.poisonErr)
	}
	effAlign := p.minAlign
	if align > effAlign {
		effAlign = align
	}
	spanSize := (size + p.minAlign - 1) &^ (p.minAlign - 1)
	if spanSize < size {
		return nil, ErrInvalidConfig
	}

	for i, s := range p.free {
		startPhys := p.base + s.offset
		alignedPhys := (startPhys + effAlign - 1) &^ (effAlign - 1)
		if alignedPhys < startPhys {
			continue
		}
		pad := alignedPhys - startPhys
		if pad > s.size || s.size-pad < spanSize {
			continue
		}
		allocOffset := s.offset + pad
		tailOffset := allocOffset + spanSize
		tailSize := s.size - pad - spanSize

		// Update free list in place, preserving sorted order.
		switch {
		case pad > 0 && tailSize > 0:
			p.free[i] = freeSpan{offset: s.offset, size: pad}
			p.free = append(p.free, freeSpan{})
			copy(p.free[i+2:], p.free[i+1:])
			p.free[i+1] = freeSpan{offset: tailOffset, size: tailSize}
		case pad > 0:
			p.free[i] = freeSpan{offset: s.offset, size: pad}
		case tailSize > 0:
			p.free[i] = freeSpan{offset: tailOffset, size: tailSize}
		default:
			p.free = append(p.free[:i], p.free[i+1:]...)
		}

		clear(p.backing[allocOffset : allocOffset+spanSize])
		p.allocatedBytes += spanSize
		p.liveBuffers++
		return &Buffer{
			pool:   p,
			offset: allocOffset,
			phys:   alignedPhys,
			size:   size,
			cap:    spanSize,
			data:   p.backing[allocOffset : allocOffset+size],
		}, nil
	}
	return nil, ErrOutOfMemory
}

func (p *Pool) insertFreeLocked(offset, size uint64) {
	// Find insertion point in sorted free slice.
	idx := 0
	for idx < len(p.free) && p.free[idx].offset < offset {
		idx++
	}
	mergeLeft := idx > 0 && p.free[idx-1].offset+p.free[idx-1].size == offset
	mergeRight := idx < len(p.free) && offset+size == p.free[idx].offset

	switch {
	case mergeLeft && mergeRight:
		p.free[idx-1].size += size + p.free[idx].size
		p.free = append(p.free[:idx], p.free[idx+1:]...)
	case mergeLeft:
		p.free[idx-1].size += size
	case mergeRight:
		p.free[idx].offset = offset
		p.free[idx].size += size
	default:
		p.free = append(p.free, freeSpan{})
		copy(p.free[idx+1:], p.free[idx:])
		p.free[idx] = freeSpan{offset: offset, size: size}
	}
}

// Buffer represents one owned DMA allocation inside a Pool.
type Buffer struct {
	pool      *Pool
	offset    uint64
	phys      uint64
	size      uint64
	cap       uint64
	data      []byte
	released  bool
	poisonErr error
}

// PhysAddr returns the physical/bus base address of this buffer.
func (b *Buffer) PhysAddr() uint64 { return b.phys }

// Len returns the caller-requested byte length of this buffer.
func (b *Buffer) Len() uint64 { return b.size }

// Cap returns the reserved span length (padded to pool alignment).
func (b *Buffer) Cap() uint64 { return b.cap }

// Mode returns the underlying pool's memory mode.
func (b *Buffer) Mode() MemoryMode { return b.pool.mode }

// Bytes returns the CPU-accessible byte slice, or nil after a clean Free.
func (b *Buffer) Bytes() []byte {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if b.released {
		return nil
	}
	return b.data
}

// Err returns the poison error affecting this buffer (or its pool), if any.
func (b *Buffer) Err() error {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if b.poisonErr != nil {
		return b.poisonErr
	}
	return b.pool.poisonErr
}

// Released reports whether Free has already returned this buffer to its pool.
func (b *Buffer) Released() bool {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	return b.released
}

// Poison permanently pins this buffer so its physical span is never recycled.
// Call this whenever a DMA transfer times out or fails with uncertain hardware
// ownership.
func (b *Buffer) Poison(err error) {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if b.released {
		return
	}
	if b.poisonErr == nil {
		if err == nil {
			err = ErrPoisoned
		}
		b.poisonErr = err
		b.pool.pinnedBytes += b.cap
		b.pool.poisonedBuffers++
	}
}

// Free returns a healthy buffer's span to its pool and coalesces adjacent free
// space. Poisoned buffers cannot be freed and stay pinned until reboot.
func (b *Buffer) Free() error {
	p := b.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if b.released {
		return ErrReleased
	}
	if b.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, b.poisonErr)
	}
	if p.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, p.poisonErr)
	}
	b.released = true
	b.data = nil
	p.allocatedBytes -= b.cap
	p.liveBuffers--
	p.insertFreeLocked(b.offset, b.cap)
	return nil
}

func (b *Buffer) checkLiveLocked() error {
	if b.released {
		return ErrReleased
	}
	if b.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, b.poisonErr)
	}
	if b.pool.poisonErr != nil {
		return fmt.Errorf("%w: %v", ErrPoisoned, b.pool.poisonErr)
	}
	return nil
}

func (b *Buffer) checkRangeLocked(offset, length uint64) error {
	if err := b.checkLiveLocked(); err != nil {
		return err
	}
	if length == 0 || offset > b.size || length > b.size-offset {
		return ErrInvalidRange
	}
	return nil
}

// Slice returns the physical address and byte view of [offset, offset+length).
func (b *Buffer) Slice(offset, length uint64) (uint64, []byte, error) {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if err := b.checkRangeLocked(offset, length); err != nil {
		return 0, nil, err
	}
	return b.phys + offset, b.data[offset : offset+length], nil
}

// Read32 reads a 4-byte-aligned little-endian uint32 at offset.
func (b *Buffer) Read32(offset uint64) (uint32, error) {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if offset%4 != 0 {
		return 0, ErrInvalidRange
	}
	if err := b.checkRangeLocked(offset, 4); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b.data[offset : offset+4]), nil
}

// Write32 writes a 4-byte-aligned little-endian uint32 at offset.
func (b *Buffer) Write32(offset uint64, val uint32) error {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if offset%4 != 0 {
		return ErrInvalidRange
	}
	if err := b.checkRangeLocked(offset, 4); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(b.data[offset:offset+4], val)
	return nil
}

// Read64 reads an 8-byte-aligned little-endian uint64 at offset.
func (b *Buffer) Read64(offset uint64) (uint64, error) {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if offset%8 != 0 {
		return 0, ErrInvalidRange
	}
	if err := b.checkRangeLocked(offset, 8); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b.data[offset : offset+8]), nil
}

// Write64 writes an 8-byte-aligned little-endian uint64 at offset.
func (b *Buffer) Write64(offset uint64, val uint64) error {
	b.pool.mu.Lock()
	defer b.pool.mu.Unlock()
	if offset%8 != 0 {
		return ErrInvalidRange
	}
	if err := b.checkRangeLocked(offset, 8); err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(b.data[offset:offset+8], val)
	return nil
}

// SyncForDevice prepares the entire buffer for device DMA in the given direction.
func (b *Buffer) SyncForDevice(dir Direction) error {
	return b.SyncRangeForDevice(0, b.size, dir)
}

// SyncRangeForDevice prepares [offset, offset+length) for device DMA.
//
// In Uncached mode it issues DSB SY. In Cached mode it cleans (DC CVAC) for
// ToDevice and cleans+invalidates (DC CIVAC) for FromDevice or Bidirectional so
// dirty cache lines cannot evict over incoming device writes.
func (b *Buffer) SyncRangeForDevice(offset, length uint64, dir Direction) error {
	if !dir.valid() {
		return ErrInvalidConfig
	}
	p := b.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := b.checkRangeLocked(offset, length); err != nil {
		return err
	}
	if p.mode == Uncached {
		p.hw.SyncBarrier()
		return nil
	}
	addr := b.phys + offset
	switch dir {
	case ToDevice:
		p.hw.CleanRange(addr, length)
	case FromDevice, Bidirectional:
		p.hw.CleanInvalidateRange(addr, length)
	}
	return nil
}

// SyncForCPU completes device DMA and makes device writes visible to the CPU.
func (b *Buffer) SyncForCPU(dir Direction) error {
	return b.SyncRangeForCPU(0, b.size, dir)
}

// SyncRangeForCPU completes device DMA over [offset, offset+length) for CPU reads.
//
// In Uncached mode it issues DMB SY. In Cached mode it invalidates (DC IVAC on
// full lines, DC CIVAC on any partial boundary lines) for FromDevice or
// Bidirectional transfers to discard speculative prefetches, and issues DMB SY
// for ToDevice.
func (b *Buffer) SyncRangeForCPU(offset, length uint64, dir Direction) error {
	if !dir.valid() {
		return ErrInvalidConfig
	}
	p := b.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := b.checkRangeLocked(offset, length); err != nil {
		return err
	}
	if p.mode == Uncached {
		p.hw.DataBarrier()
		return nil
	}
	addr := b.phys + offset
	switch dir {
	case ToDevice:
		p.hw.DataBarrier()
	case FromDevice, Bidirectional:
		p.hw.InvalidateRange(addr, length)
	}
	return nil
}
