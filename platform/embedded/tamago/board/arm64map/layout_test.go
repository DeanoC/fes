// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "testing"

func TestExplicitNonOverlappingWindows(t *testing.T) {
	for _, v := range [][4]uint64{{0x50000000, 128 << 20, 0x40000000, 256 << 20}, {0x92000000, 2 << 20, 0x82000000, 256 << 20}} {
		if err := Validate(v[0], v[1], v[2], v[3]); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range [][4]uint64{{0x80000000, 2 << 20, 0x80000000, 256 << 20}, {0x92001000, 2 << 20, 0x82000000, 256 << 20}, {0xc0000000, 2 << 20, 0x82000000, 256 << 20}, {^uint64(0) - 4095, 8192, 0x82000000, 256 << 20}, {0x92000000, 0, 0x82000000, 256 << 20}} {
		if err := Validate(v[0], v[1], v[2], v[3]); err == nil {
			t.Fatal("unsafe mapping", v)
		}
	}
}
