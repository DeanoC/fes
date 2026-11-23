// SPDX-License-Identifier: GPL-3.0-or-later

// Package dmaops provides ARM64 DMA cache-coherency primitives, aligned buffer
// pools with fail-closed poison semantics, and fixed descriptor rings for
// bare-metal TamaGo drivers on QEMU virt and DE25-Nano (Agilex 5).
package dmaops

import "errors"

// MemoryMode describes the EL1 stage-1 memory attribute of a DMA arena.
type MemoryMode uint8

const (
	// Uncached selects Normal Non-Cacheable memory (MAIR attribute 0x44).
	// Sync operations issue barriers without cache maintenance instructions.
	Uncached MemoryMode = iota
	// Cached selects Normal Write-Back Cacheable memory. Sync operations issue
	// Point-of-Coherency cache clean/invalidate maintenance and barriers, and
	// allocations are padded and aligned to CacheLineSize so distinct buffers
	// never share a cache line.
	Cached
)

func (m MemoryMode) String() string {
	switch m {
	case Uncached:
		return "uncached"
	case Cached:
		return "cached"
	default:
		return "unknown"
	}
}

// Direction describes the data flow of a DMA transfer relative to the device.
type Direction uint8

const (
	// ToDevice means the CPU wrote the buffer and the device will read it.
	ToDevice Direction = iota
	// FromDevice means the device will write the buffer and the CPU will read it.
	FromDevice
	// Bidirectional means both CPU and device read and write the buffer.
	Bidirectional
)

func (d Direction) valid() bool {
	return d == ToDevice || d == FromDevice || d == Bidirectional
}

// Coherency abstracts ARM64 cache-line geometry, memory barriers, and
// Point-of-Coherency data cache maintenance so Pool and Ring can be unit-tested
// on the host.
type Coherency interface {
	// CacheLineSize returns the minimum data cache line size in bytes (DminLine).
	CacheLineSize() uint64
	// DataBarrier executes DMB SY.
	DataBarrier()
	// SyncBarrier executes DSB SY.
	SyncBarrier()
	// CleanRange cleans [addr, addr+size) to the Point of Coherency (DC CVAC + DSB SY).
	CleanRange(addr, size uint64)
	// InvalidateRange invalidates [addr, addr+size) from the data cache (DC IVAC on
	// full cache lines, DC CIVAC on any unaligned boundary lines, followed by DSB SY).
	InvalidateRange(addr, size uint64)
	// CleanInvalidateRange cleans and invalidates [addr, addr+size) to the Point of
	// Coherency (DC CIVAC + DSB SY).
	CleanInvalidateRange(addr, size uint64)
}

var (
	ErrInvalidConfig = errors.New("dmaops: invalid configuration")
	ErrInvalidRange  = errors.New("dmaops: invalid offset or range")
	ErrOutOfMemory   = errors.New("dmaops: pool exhausted")
	ErrPoisoned      = errors.New("dmaops: DMA resource poisoned and permanently pinned")
	ErrReleased      = errors.New("dmaops: DMA resource already released")
	ErrInFlight      = errors.New("dmaops: DMA transfer still in flight")
	ErrRingFull      = errors.New("dmaops: descriptor ring full")
	ErrRingEmpty     = errors.New("dmaops: descriptor ring empty")
	ErrInvalidSlot   = errors.New("dmaops: invalid or out-of-order ring slot")
)

const (
	minAlignUncached = 8
	minCacheLine     = 16
	maxCacheLine     = 4096
)

func isPowerOfTwo(v uint64) bool {
	return v != 0 && v&(v-1) == 0
}

func validCacheLine(line uint64) bool {
	return line >= minCacheLine && line <= maxCacheLine && isPowerOfTwo(line)
}

// CacheLineSizeFromCTR decodes CTR_EL0.DminLine (bits 19:16), which encodes
// log2 of the smallest data/unified cache line size in 4-byte words.
func CacheLineSizeFromCTR(ctr uint64) uint64 {
	dmin := (ctr >> 16) & 0xf
	return uint64(4) << dmin
}

// AlignRange rounds [addr, addr+size) outward to multiples of line.
// It returns ok=false on zero size, invalid line, or uint64 overflow.
func AlignRange(addr, size, line uint64) (start, end uint64, ok bool) {
	if size == 0 || !isPowerOfTwo(line) {
		return 0, 0, false
	}
	mask := line - 1
	start = addr &^ mask
	rawEnd := addr + size
	if rawEnd < addr {
		return 0, 0, false
	}
	end = (rawEnd + mask) &^ mask
	if end < rawEnd {
		return 0, 0, false
	}
	return start, end, true
}

// InvalidatePlan splits [addr, addr+size) into safe cache-maintenance sub-ranges:
// any partial head or tail cache line is cleaned-and-invalidated (DC CIVAC) so
// dirty neighbor bytes sharing that line are never discarded, while full interior
// cache lines are invalidated directly (DC IVAC).
type InvalidatePlan struct {
	HeadStart uint64
	HasHead   bool
	MidStart  uint64
	MidEnd    uint64
	HasMid    bool
	TailStart uint64
	HasTail   bool
}

// PlanInvalidate computes a safe cache-invalidation plan for [addr, addr+size).
func PlanInvalidate(addr, size, line uint64) (InvalidatePlan, bool) {
	start, end, ok := AlignRange(addr, size, line)
	if !ok {
		return InvalidatePlan{}, false
	}
	rawEnd := addr + size
	headPartial := addr != start
	tailPartial := rawEnd != end

	var p InvalidatePlan
	curStart := start
	curEnd := end

	if headPartial {
		p.HasHead = true
		p.HeadStart = start
		curStart = start + line
		if curStart >= curEnd {
			// Entire span lies within one single cache line; Head covers it.
			return p, true
		}
	}
	if tailPartial {
		p.HasTail = true
		p.TailStart = curEnd - line
		curEnd -= line
	}
	if curStart < curEnd {
		p.HasMid = true
		p.MidStart = curStart
		p.MidEnd = curEnd
	}
	return p, true
}
