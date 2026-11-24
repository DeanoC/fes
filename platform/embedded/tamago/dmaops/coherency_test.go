// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import (
	"sync"
	"testing"
)

type coherencyOp struct {
	kind string
	addr uint64
	size uint64
}

type mockCoherency struct {
	mu   sync.Mutex
	line uint64
	ops  []coherencyOp
}

func newMockCoherency(line uint64) *mockCoherency {
	return &mockCoherency{line: line}
}

func (m *mockCoherency) CacheLineSize() uint64 { return m.line }

func (m *mockCoherency) record(kind string, addr, size uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, coherencyOp{kind: kind, addr: addr, size: size})
}

func (m *mockCoherency) DataBarrier() { m.record("dmb", 0, 0) }
func (m *mockCoherency) SyncBarrier() { m.record("dsb", 0, 0) }
func (m *mockCoherency) CleanRange(addr, size uint64) {
	m.record("clean", addr, size)
}
func (m *mockCoherency) InvalidateRange(addr, size uint64) {
	m.record("inv", addr, size)
}
func (m *mockCoherency) CleanInvalidateRange(addr, size uint64) {
	m.record("clean_inv", addr, size)
}

func (m *mockCoherency) take() []coherencyOp {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]coherencyOp(nil), m.ops...)
	m.ops = nil
	return out
}

func TestMemoryModeString(t *testing.T) {
	if Uncached.String() != "uncached" || Cached.String() != "cached" || MemoryMode(99).String() != "unknown" {
		t.Fatalf("unexpected MemoryMode.String values: %q %q %q", Uncached.String(), Cached.String(), MemoryMode(99).String())
	}
}

func TestCacheLineSizeFromCTR(t *testing.T) {
	cases := []struct {
		ctr  uint64
		want uint64
	}{
		{0x0 << 16, 4},
		{0x2 << 16, 16},
		{0x3 << 16, 32},
		{0x4 << 16, 64},  // Cortex-A53 / A55 / A76
		{0x5 << 16, 128}, // 128-byte lines
		{0x84448004, 64}, // typical Cortex-A53/A55 CTR_EL0 value
	}
	for _, tc := range cases {
		if got := CacheLineSizeFromCTR(tc.ctr); got != tc.want {
			t.Fatalf("CTR %#x: got %d, want %d", tc.ctr, got, tc.want)
		}
	}
}

func TestAlignRange(t *testing.T) {
	start, end, ok := AlignRange(0x1000, 64, 64)
	if !ok || start != 0x1000 || end != 0x1040 {
		t.Fatalf("aligned: got (%#x, %#x, %v)", start, end, ok)
	}

	start, end, ok = AlignRange(0x1003, 10, 64)
	if !ok || start != 0x1000 || end != 0x1040 {
		t.Fatalf("within one line: got (%#x, %#x, %v)", start, end, ok)
	}

	start, end, ok = AlignRange(0x1038, 16, 64)
	if !ok || start != 0x1000 || end != 0x1080 {
		t.Fatalf("straddle two lines: got (%#x, %#x, %v)", start, end, ok)
	}

	if _, _, ok := AlignRange(0x1000, 0, 64); ok {
		t.Fatal("zero size should fail")
	}
	if _, _, ok := AlignRange(0x1000, 64, 48); ok {
		t.Fatal("non-power-of-two line should fail")
	}
	if _, _, ok := AlignRange(^uint64(0)-10, 32, 64); ok {
		t.Fatal("address overflow should fail")
	}
	if _, _, ok := AlignRange(^uint64(0)-32, 16, 64); ok {
		t.Fatal("rounded end overflow should fail")
	}
}

func TestPlanInvalidate(t *testing.T) {
	// Fully aligned 3 lines: pure DC IVAC.
	p, ok := PlanInvalidate(0x2000, 192, 64)
	if !ok || p.HasHead || p.HasTail || !p.HasMid || p.MidStart != 0x2000 || p.MidEnd != 0x20c0 {
		t.Fatalf("aligned 3 lines: %+v ok=%v", p, ok)
	}

	// Single partial line: single DC CIVAC on head only.
	p, ok = PlanInvalidate(0x2008, 16, 64)
	if !ok || !p.HasHead || p.HeadStart != 0x2000 || p.HasMid || p.HasTail {
		t.Fatalf("single partial line: %+v ok=%v", p, ok)
	}

	// Unaligned head + aligned end across 2 lines: head CIVAC + mid IVAC.
	p, ok = PlanInvalidate(0x2010, 0x70, 64)
	if !ok || !p.HasHead || p.HeadStart != 0x2000 || !p.HasMid || p.MidStart != 0x2040 || p.MidEnd != 0x2080 || p.HasTail {
		t.Fatalf("unaligned head: %+v ok=%v", p, ok)
	}

	// Aligned start + unaligned tail across 2 lines: mid IVAC + tail CIVAC.
	p, ok = PlanInvalidate(0x2000, 0x50, 64)
	if !ok || p.HasHead || !p.HasMid || p.MidStart != 0x2000 || p.MidEnd != 0x2040 || !p.HasTail || p.TailStart != 0x2040 {
		t.Fatalf("unaligned tail: %+v ok=%v", p, ok)
	}

	// Straddling exactly 2 partial lines: head CIVAC + tail CIVAC, no mid.
	p, ok = PlanInvalidate(0x2030, 0x20, 64)
	if !ok || !p.HasHead || p.HeadStart != 0x2000 || p.HasMid || !p.HasTail || p.TailStart != 0x2040 {
		t.Fatalf("two partial lines: %+v ok=%v", p, ok)
	}

	// Head partial + 3 full interior lines + tail partial (0x2010..0x2110).
	p, ok = PlanInvalidate(0x2010, 0x100, 64)
	if !ok || !p.HasHead || p.HeadStart != 0x2000 || !p.HasMid || p.MidStart != 0x2040 || p.MidEnd != 0x2100 || !p.HasTail || p.TailStart != 0x2100 {
		t.Fatalf("head+mid+tail: %+v ok=%v", p, ok)
	}

	// Invalid inputs.
	if _, ok := PlanInvalidate(0x2000, 0, 64); ok {
		t.Fatal("zero size should fail")
	}
	if _, ok := PlanInvalidate(^uint64(0)-10, 20, 64); ok {
		t.Fatal("overflow should fail")
	}
}
