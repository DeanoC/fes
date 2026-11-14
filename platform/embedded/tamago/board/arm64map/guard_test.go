// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import (
	"errors"
	"testing"
)

func TestGuardBracketsOneOperationAndPublishesOnlyUnchangedSuccess(t *testing.T) {
	for _, mode := range []string{"success", "before", "after", "root", "attribute", "digest", "cpu"} {
		calls, issued := 0, 0
		baseline := Observation{CPU: CPUState{TTBR0: 0x84004000, MAIR: 0x44ff00}, DescriptorSHA256: [32]byte{1}}
		observe := func() (Observation, error) {
			calls++
			if mode == "before" && calls == 1 || mode == "after" && calls == 2 {
				return Observation{}, errors.New("mapping unavailable")
			}
			v := baseline
			if calls == 2 {
				switch mode {
				case "root":
					v.CPU.TTBR0 += 4096
				case "attribute":
					v.CPU.MAIR ^= 1 << 16
				case "digest":
					v.DescriptorSHA256[0] ^= 1
				case "cpu":
					v.CPU.MPIDR++
				}
			}
			return v, nil
		}
		value, err := WithStableMapping(observe, func() [4]uint64 {
			issued++
			if calls != 1 {
				t.Fatal("operation was not bracketed")
			}
			return [4]uint64{1, 2, 3, 4}
		})
		if mode == "success" {
			if err != nil || value != [4]uint64{1, 2, 3, 4} || issued != 1 || calls != 2 {
				t.Fatal(mode, value, err, calls, issued)
			}
		} else {
			wantIssued := 1
			if mode == "before" {
				wantIssued = 0
			}
			if err == nil || value != [4]uint64{} || issued != wantIssued {
				t.Fatal(mode, value, err, calls, issued)
			}
		}
	}
	if _, err := WithStableMapping[uint32](nil, func() uint32 { t.Fatal("unguarded operation issued"); return 0 }); err == nil {
		t.Fatal("missing guard accepted")
	}
}
