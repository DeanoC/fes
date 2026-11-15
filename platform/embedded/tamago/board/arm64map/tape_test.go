// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "testing"

func TestIndependentTapeMappingGuardsAllPairedResources(t *testing.T) {
	ranges := []Range{{0x94800000, 8 << 20, Uncached}, {0x95000000, 8 << 20, Uncached}, {0x95800000, 2 << 20, Uncached}}
	for _, address := range []uint64{0x94800000, 0x94e00000, 0x95000000, 0x95600000, 0x95800000} {
		for _, during := range []bool{false, true} {
			for fault := 0; fault < 3; fault++ {
				m := excludedTables()
				mapper := protectedMapper(t, m)
				for _, r := range ranges {
					if _, err := mapper.Map(r.Base, r.Bytes); err != nil {
						t.Fatal(err)
					}
				}
				mutate := func() {
					entry := uint64(0x84006000) + ((address>>21)&511)*8
					switch fault {
					case 0:
						m.words[entry] &^= 7 << 2 // Device alias.
					case 1:
						m.words[entry] ^= 1 << 21 // Different physical storage.
					case 2:
						m.words[entry] &^= xn // Executable tape/data.
					}
				}
				if !during {
					mutate()
				}
				calls := 0
				result, err := WithStableMapping(func() (Observation, error) { return mapper.Inspect(ranges) }, func() int {
					calls++
					mutate()
					return 42
				})
				want := 0
				if during {
					want = 1
				}
				if err == nil || result != 0 || calls != want {
					t.Fatal(address, during, fault, result, calls, err)
				}
			}
		}
	}
}
