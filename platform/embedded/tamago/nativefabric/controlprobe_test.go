// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func controlMapFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/hps-control.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func controlProbeCPU(t *testing.T, replies [][4]uint64, failure int, calls *int) FirmwareCaller {
	t.Helper()
	ops := []uint64{0, 4, 5, 0, 6, 0}
	return bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
		i := *calls
		*calls++
		if i >= len(ops) {
			t.Fatal("identity probe retried")
		}
		want := [7]uint64{0xc2000301, ops[i], 17, 0x46455342, replies[0][3]}
		if ops[i] == 0 {
			want[2], want[4] = 0, 0
		}
		if request != want {
			t.Fatalf("identity probe used another channel/address: %x", request)
		}
		if i == failure {
			return [4]uint64{}, errors.New("lost identity transport")
		}
		return replies[i], nil
	})
}
func controlProbeReplies(raw []byte) [][4]uint64 {
	return append(hpsContextReplies(sha256.Sum256(raw)),
		[4]uint64{0, 0x4645493110203040, 0xe25bb65a1d14a2bb, 0xbddf4c770ea033e5},
		[4]uint64{0, 0x46424f310000009f, 17, 3})
}
func TestControlProbeRequiresCompiledSourceAndClosedReturnedScope(t *testing.T) {
	raw := hpsContextFixture(t)
	calls := 0
	v, err := NewHPSContextVerifier(controlProbeCPU(t, controlProbeReplies(raw), -1, &calls), raw)
	if err != nil {
		t.Fatal(err)
	}
	mapping := controlMapFixture(t)
	observed, err := v.ProbeIdentity(mapping)
	if err != nil || calls != 6 || observed.Source.Bridges.Generation != 1 || observed.After.Generation != 3 || observed.After.ResetMask != 15 || !observed.After.MastersContained || observed.After.NeverEnabled {
		t.Fatal(observed, err, calls)
	}
	if observed.CPUBase != 0x20010400 || observed.ControlMapSHA256 != sha256.Sum256(mapping) {
		t.Fatal(observed)
	}
	build, _ := BuildWords(v.Identity().BuildID)
	if observed.Identity.Build != build || observed.Identity.ABI != 0x10203040 {
		t.Fatal(observed)
	}
	if _, ok := interface{}(v).(BootHandoffVerifier); ok {
		t.Fatal("identity diagnostic became live boot/programming authority")
	}
}
func TestControlProbeRejectsPartialUnknownAndChangedObservationsWithoutRetry(t *testing.T) {
	for _, name := range []string{"unready", "busy", "transport", "tag", "abi", "build-high", "build-low", "late-owner", "late-generation", "late-mask", "late-masters", "late-capability", "late-never", "late-transport", "not-cold", "not-first-generation"} {
		t.Run(name, func(t *testing.T) {
			raw := hpsContextFixture(t)
			replies := controlProbeReplies(raw)
			failure, maximum := -1, 5
			switch name {
			case "unready":
				replies[4][0] = 5
			case "busy":
				replies[4][0] = 1
			case "transport":
				failure = 4
			case "tag":
				replies[4][1] = 0x46424f3110203040
			case "abi":
				replies[4][1] ^= 1
			case "build-high":
				replies[4][2] ^= 1
			case "build-low":
				replies[4][3] ^= 1
			case "late-owner":
				replies[5][2]++
				maximum = 6
			case "late-generation":
				replies[5][3] = 1
				maximum = 6
			case "late-mask":
				replies[5][1] &= ^uint64(2)
				maximum = 6
			case "late-masters":
				replies[5][1] &= ^uint64(0x80)
				maximum = 6
			case "late-capability":
				replies[5][1] = 0x46424f31000000af
				maximum = 6
			case "late-never":
				replies[5][1] |= 0x40
				maximum = 6
			case "late-transport":
				failure = 5
				maximum = 6
			case "not-cold":
				replies[0][1] = 0x46424f310000009f
				replies[3] = replies[0]
				maximum = 4
			case "not-first-generation":
				replies[0][3] = 3
				replies[3] = replies[0]
				maximum = 4
			}
			calls := 0
			v, err := NewHPSContextVerifier(controlProbeCPU(t, replies, failure, &calls), raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.ProbeIdentity(controlMapFixture(t))
			if err == nil || observed != (FPGAIdentityObservation{}) || calls != maximum {
				t.Fatal(observed, err, calls, maximum)
			}
		})
	}
}
func TestControlProbeRejectsUncompiledMapBeforeFirmware(t *testing.T) {
	for _, name := range []string{"empty", "unknown", "missing", "address", "package", "handoff", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			mapping := controlMapFixture(t)
			var m map[string]interface{}
			if err := json.Unmarshal(mapping, &m); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "empty":
				mapping = nil
			case "duplicate":
				mapping = append([]byte("{\"format\":1,"), mapping[1:]...)
			default:
				switch name {
				case "unknown":
					m["permission"] = true
				case "missing":
					delete(m, "bridge_base")
				case "address":
					m["cpu_base"] = float64(0xff210400)
				case "package":
					m["package_id"] = "f"
				case "handoff":
					m["handoff_sha256"] = "0"
				}
				mapping, _ = json.Marshal(m)
				mapping = append(mapping, '\n')
			}
			calls := 0
			v, err := NewHPSContextVerifier(controlProbeCPU(t, nil, -1, &calls), hpsContextFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = v.ProbeIdentity(mapping); err == nil || calls != 0 {
				t.Fatal(err, calls)
			}
		})
	}
}
