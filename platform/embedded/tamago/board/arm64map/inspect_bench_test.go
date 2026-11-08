// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "testing"

func TestControlObservationUsesBoundedScratchAllocations(t *testing.T) {
	m := excludedTables()
	ranges := []Range{{0x20010400, 4, Device}, firmwareRAMRange()}
	first := inspector(t, m, ranges)
	allocations := testing.AllocsPerRun(10, func() {
		got, err := Inspect(m, 0x84000000, 256<<20, ranges)
		if err != nil || got != first {
			t.Fatalf("observation changed: %+v, %v", got, err)
		}
	})
	// Leave room for compiler bookkeeping, but reject per-descriptor storage.
	if allocations > 4 {
		t.Fatalf("control observation allocated %.0f times", allocations)
	}
}

// These benchmarks include the complete observation used on either side of a
// Nano CSR access. They measure host implementation cost, not board latency.
func BenchmarkInspectControlMapping(b *testing.B) {
	m := excludedTables()
	ranges := []Range{{0x20010400, 4, Device}, firmwareRAMRange()}
	first, err := Inspect(m, 0x84000000, 256<<20, ranges)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("observation=%+v", first)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		got, err := Inspect(m, 0x84000000, 256<<20, ranges)
		if err != nil || got != first {
			b.Fatalf("observation changed: %+v, %v", got, err)
		}
	}
}
