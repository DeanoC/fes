// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import "testing"

func TestReleaseWatchdogResetSelectsOnlyRequestedReset(t *testing.T) {
	const initial = uint32(1<<0 | 1<<1 | 1<<9)

	for _, tc := range []struct {
		name  string
		index int
		mask  uint32
	}{
		{name: "L4WD0", index: 0, mask: agilex5L4WD0ResetMask},
		{name: "L4WD1", index: 1, mask: agilex5L4WD1ResetMask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := agilex5RSTMGRBase + agilex5PER1MODRSTOffset
			var reads, writes int
			var writtenAddr uintptr
			var writtenValue uint32
			if !releaseWatchdogReset(tc.index, func(got uintptr) uint32 {
				reads++
				if got != addr {
					t.Fatalf("read address=%#x, want %#x", got, addr)
				}
				return initial
			}, func(got uintptr, value uint32) {
				writes++
				writtenAddr, writtenValue = got, value
			}) {
				t.Fatal("releaseWatchdogReset returned false")
			}
			if reads != 1 || writes != 1 {
				t.Fatalf("reads=%d writes=%d, want one read and write", reads, writes)
			}
			if writtenAddr != addr || writtenValue != initial&^tc.mask {
				t.Fatalf("write=(%#x, %#x), want (%#x, %#x)", writtenAddr, writtenValue, addr, initial&^tc.mask)
			}
		})
	}
}

func TestReleaseWatchdogResetRejectsInvalidIndexWithoutMMIO(t *testing.T) {
	for _, index := range []int{-1, 2} {
		if releaseWatchdogReset(index, func(uintptr) uint32 {
			t.Fatal("unexpected MMIO read")
			return 0
		}, func(uintptr, uint32) {
			t.Fatal("unexpected MMIO write")
		}) {
			t.Fatalf("index %d accepted", index)
		}
	}
}
