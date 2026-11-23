//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import "sync/atomic"

var cachedLineSize atomic.Uint64

type nativeCoherency struct{}

// Native returns the ARM64 EL1 coherency provider backed by CTR_EL0, DMB/DSB,
// and DC CVAC/IVAC/CIVAC instructions.
func Native() Coherency { return nativeCoherency{} }

// CacheLineSize returns the hardware minimum data cache line size from CTR_EL0.
func CacheLineSize() uint64 { return nativeCoherency{}.CacheLineSize() }

// DataBarrier executes DMB SY.
func DataBarrier() { dmbSY() }

// SyncBarrier executes DSB SY.
func SyncBarrier() { dsbSY() }

func (nativeCoherency) CacheLineSize() uint64 {
	if v := cachedLineSize.Load(); v != 0 {
		return v
	}
	line := CacheLineSizeFromCTR(readCTR())
	cachedLineSize.Store(line)
	return line
}

func (nativeCoherency) DataBarrier() { dmbSY() }
func (nativeCoherency) SyncBarrier() { dsbSY() }

func (n nativeCoherency) CleanRange(addr, size uint64) {
	line := n.CacheLineSize()
	start, end, ok := AlignRange(addr, size, line)
	if !ok {
		return
	}
	dcCleanRange(start, end, line)
}

func (n nativeCoherency) InvalidateRange(addr, size uint64) {
	line := n.CacheLineSize()
	plan, ok := PlanInvalidate(addr, size, line)
	if !ok {
		return
	}
	if plan.HasHead {
		dcCleanInvalidateRange(plan.HeadStart, plan.HeadStart+line, line)
	}
	if plan.HasMid {
		dcInvalidateRange(plan.MidStart, plan.MidEnd, line)
	}
	if plan.HasTail {
		dcCleanInvalidateRange(plan.TailStart, plan.TailStart+line, line)
	}
}

func (n nativeCoherency) CleanInvalidateRange(addr, size uint64) {
	line := n.CacheLineSize()
	start, end, ok := AlignRange(addr, size, line)
	if !ok {
		return
	}
	dcCleanInvalidateRange(start, end, line)
}

func readCTR() uint64
func dmbSY()
func dsbSY()
func dcCleanRange(start, end, stride uint64)
func dcInvalidateRange(start, end, stride uint64)
func dcCleanInvalidateRange(start, end, stride uint64)
