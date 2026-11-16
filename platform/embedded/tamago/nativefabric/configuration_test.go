// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"errors"
	"testing"
)

func TestConfigurationQueryKeepsClosedOwnershipAndRejectsPartialReplies(t *testing.T) {
	for _, name := range []string{"unready", "ready", "status", "tag", "bits", "owner", "generation", "after-owner", "after-mask", "after-transport", "query-transport", "query-stale-generation"} {
		t.Run(name, func(t *testing.T) {
			raw := hpsContextFixture(t)
			replies := append(hpsContextReplies(sha256.Sum256(raw)), [4]uint64{0, 0x4643533100000000, 17, 1})
			replies = append(replies, replies[0])
			failure, maximum := -1, 5
			switch name {
			case "ready":
				replies[4][1]++
			case "status":
				replies[4][0] = 5
			case "tag":
				replies[4][1] = 0x4645493100000000
			case "bits":
				replies[4][1] |= 2
			case "owner":
				replies[4][2]++
			case "generation":
				replies[4][3]++
			case "after-owner":
				replies[5][2]++
				maximum = 6
			case "after-mask":
				replies[5][1] &= ^uint64(2)
				maximum = 6
			case "after-transport":
				failure = 5
				maximum = 6
			case "query-transport":
				failure = 4
			case "query-stale-generation":
				replies[0][3] = 3
				replies[3] = replies[0]
			}
			calls := 0
			ops := []uint64{0, 4, 5, 0, 7, 0}
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				i := calls
				calls++
				if i >= len(ops) {
					t.Fatal("configuration query retried")
				}
				want := [7]uint64{0xc2000301, ops[i], 17, 0x46455342, replies[0][3]}
				if ops[i] == 0 {
					want[2], want[4] = 0, 0
				}
				if a != want {
					t.Fatalf("configuration query changed operation or address: %x", a)
				}
				if i == failure {
					return [4]uint64{}, errors.New("lost query reply")
				}
				return replies[i], nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.Configuration(controlMapFixture(t))
			if name == "unready" || name == "ready" {
				if err != nil || calls != 6 || observed.Configured != (name == "ready") || observed.Source.Bridges.Generation != 1 || !observed.Source.Bridges.NeverEnabled {
					t.Fatal(observed, err, calls)
				}
				if _, ok := interface{}(v).(BootHandoffVerifier); ok {
					t.Fatal("status observation became boot admission")
				}
			} else if err == nil || observed != (FPGAConfigurationObservation{}) || calls != maximum {
				t.Fatal(observed, err, calls, maximum)
			}
		})
	}
	called := false
	v, err := NewHPSContextVerifier(bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) { called = true; return [4]uint64{}, nil }), hpsContextFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Configuration(nil); err == nil || called {
		t.Fatal("uncompiled configuration map reached firmware")
	}
}
