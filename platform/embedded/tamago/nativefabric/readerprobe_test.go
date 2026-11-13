// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"testing"
)

func TestReaderProbeBindsDedicatedOperationAndReclosedStorageGeneration(t *testing.T) {
	for _, generation := range []uint64{1, 4} {
		raw := hpsContextFixture(t)
		replies := controlProbeReplies(raw)
		replies[0][3], replies[3][3], replies[5][3] = generation, generation, generation+2
		replies[4][1] = 0x4652443110203040
		calls := 0
		v, err := NewHPSContextVerifier(probeCPU(t, replies, -1, &calls, 10), raw)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := v.ProbeReader(controlMapFixture(t))
		if err != nil || calls != 6 || observed.Source.Bridges.Generation != generation || observed.After.Generation != generation+2 || observed.After.ResetMask != 15 || !observed.After.MastersContained || observed.After.NeverEnabled {
			t.Fatal(observed, err, calls)
		}
		if _, ok := interface{}(v).(BootHandoffVerifier); ok {
			t.Fatal("on-chip reader diagnostic became boot admission")
		}
	}
}

func TestReaderProbeRejectsIdentityOnlyReplyAndChangedOwnershipWithoutRetry(t *testing.T) {
	for _, name := range []string{"unready", "partial-unready", "busy", "transport", "identity-only-tag", "abi", "build", "late-owner", "late-generation", "late-mask", "late-masters", "late-transport", "not-cold", "overflow"} {
		t.Run(name, func(t *testing.T) {
			raw := hpsContextFixture(t)
			replies := controlProbeReplies(raw)
			replies[4][1] = 0x4652443110203040
			failure, maximum := -1, 5
			switch name {
			case "unready":
				replies[4] = [4]uint64{5, 0x4652443100000000}
			case "partial-unready":
				replies[4] = [4]uint64{5, 0x4652443100000000, 17, 1}
			case "busy":
				replies[4][0] = 1
			case "transport":
				failure = 4
			case "identity-only-tag":
				replies[4][1] = 0x4645493110203040
			case "abi":
				replies[4][1] ^= 1
			case "build":
				replies[4][2] ^= 1
			case "late-owner":
				replies[5][2]++
				maximum = 6
			case "late-generation":
				replies[5][3]++
				maximum = 6
			case "late-mask":
				replies[5][1] &= ^uint64(2)
				maximum = 6
			case "late-masters":
				replies[5][1] &= ^uint64(0x80)
				maximum = 6
			case "late-transport":
				failure, maximum = 5, 6
			case "not-cold":
				replies[0][1] &= ^uint64(0x40)
				replies[3] = replies[0]
				maximum = 4
			case "overflow":
				replies[0][3] = ^uint64(0) - 1
				replies[3] = replies[0]
				maximum = 4
			}
			calls := 0
			v, err := NewHPSContextVerifier(probeCPU(t, replies, failure, &calls, 10), raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.ProbeReader(controlMapFixture(t))
			if err == nil || observed != (FPGAIdentityObservation{}) || calls != maximum {
				t.Fatal(observed, err, calls, maximum)
			}
			if errors.Is(err, ErrIdentityNotReady) != (name == "unready") {
				t.Fatal("partial/error reply reported a recognized unready reader", err)
			}
		})
	}
	for _, raw := range [][]byte{nil, []byte("{}\n")} {
		calls := 0
		v, err := NewHPSContextVerifier(probeCPU(t, nil, -1, &calls, 10), hpsContextFixture(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = v.ProbeReader(raw); err == nil || calls != 0 {
			t.Fatal("uncompiled reader map reached firmware", err, calls)
		}
	}
}
