// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func ddrProbeMaps(t *testing.T) (control, memory, contract []byte) {
	t.Helper()
	var c map[string]interface{}
	if err := json.Unmarshal(controlMapFixture(t), &c); err != nil {
		t.Fatal(err)
	}
	c["package_id"], c["build_id"] = strings.Repeat("d", 64), strings.Repeat("e", 32)
	c["fabric_stop"] = map[string]interface{}{"scope": "sealed-ddr-master-stop-v1", "probe_cpu_base": 0x20010100, "probe_span": 128, "maintenance": "held-reset-from-configuration"}
	control, _ = json.Marshal(c)
	control = append(control, '\n')
	memory = readerMemoryFixture(t)
	mh, ch := sha256.Sum256(memory), sha256.Sum256(control)
	contract, _ = json.Marshal(map[string]interface{}{"format": 1, "scope": "sealed-cold-ddr-read-v1",
		"package_id": c["package_id"], "build_id": c["build_id"], "reader_memory_sha256": hex.EncodeToString(mh[:]),
		"control_map_sha256": hex.EncodeToString(ch[:]), "adapter_source_sha256": strings.Repeat("a", 64),
		"adapter_generated_sha256": strings.Repeat("b", 64), "arcache": 2, "arprot": 3, "aruser": 224,
		"read_bytes": 32, "open_mask": 8, "close_mask": 10})
	return control, memory, append(contract, '\n')
}

func TestDDRProbePinsMemoryAndRequiresThreeClosedTransitionsWithoutReplay(t *testing.T) {
	for _, fault := range []string{"pass", "transport0", "transport1", "transport2", "transport3", "transport4", "transport5",
		"identity-tag", "onchip-abi", "stale-build", "unready", "partial-unready", "busy", "two-transitions",
		"late-owner", "late-active", "late-capability", "late-pattern", "overflow", "not-cold"} {
		t.Run(fault, func(t *testing.T) {
			raw := ddrContextFixture(t)
			control, memory, contract := ddrProbeMaps(t)
			calls, releases := 0, 0
			v, err := NewHPSContextVerifier(readerMemoryCPU(t, raw, -1, &calls, nil), raw)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.ReserveReaderMemory(memory, func(data []byte) (*Region, error) {
				return &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}, nil
			})
			if err != nil || calls != 8 {
				t.Fatal(err, calls)
			}
			replies := controlProbeReplies(raw)
			replies[0][3], replies[3][3], replies[5][3] = 4, 4, 7
			replies[4] = [4]uint64{0, 0x4652443110203041, 0xeeeeeeeeeeeeeeee, 0xeeeeeeeeeeeeeeee}
			failure := -1
			if strings.HasPrefix(fault, "transport") {
				failure = int(fault[len(fault)-1] - '0')
			}
			switch fault {
			case "identity-tag":
				replies[4][1] = 0x4645493110203041
			case "onchip-abi":
				replies[4][1]--
			case "stale-build":
				replies[4][2]--
			case "unready":
				replies[4] = [4]uint64{5, 0x4652443100000000}
			case "partial-unready":
				replies[4] = [4]uint64{5, 0x4652443100000000, 17, 4}
			case "busy":
				replies[4][0] = 1
			case "two-transitions":
				replies[5][3] = 6
			case "late-owner":
				replies[5][2]++
			case "late-active":
				replies[5][1] &= ^uint64(0x80)
			case "late-capability":
				replies[5][1] = 0x46424f31000000af
			case "overflow":
				replies[0][3] = ^uint64(0) - 2
				replies[3] = replies[0]
			case "not-cold":
				replies[0][1] &= ^uint64(0x40)
				replies[3] = replies[0]
			}
			calls = 0
			cpu := probeCPU(t, replies, failure, &calls, 10)
			v.cpu = bridgeFirmwareFunc(func(r [7]uint64) ([4]uint64, error) {
				value, err := cpu.Invoke(r)
				if calls == 6 && fault == "late-pattern" {
					v.readerMemoryRegion.Bytes[8191] ^= 1
				}
				return value, err
			})
			got, err := v.ProbeDDRReader(control, memory, contract)
			if fault == "pass" {
				if err != nil || got.After.Generation != 7 || !got.After.MastersContained || got.After.ResetMask != 15 || got.After.NeverEnabled || calls != 6 {
					t.Fatal(got, err, calls)
				}
			} else if err == nil || got != (FPGAIdentityObservation{}) {
				t.Fatal("uncertain DDR probe accepted", got, err)
			}
			if errors.Is(err, ErrIdentityNotReady) != (fault == "unready") {
				t.Fatal("partial reply was treated as unready", err)
			}
			before := calls
			if _, err = v.ProbeDDRReader(control, memory, contract); err == nil || calls != before || releases != 0 {
				t.Fatal("DDR read replayed or released memory", err, calls, releases)
			}
		})
	}
}

func TestDDRProbeRejectsUnqualifiedMapsAndFailedReservationBeforeFirmware(t *testing.T) {
	for _, fault := range []string{"missing-pin", "failed-pin", "pattern", "address", "missing-contract", "unknown", "duplicate", "attributes", "close-mask", "memory-map"} {
		t.Run(fault, func(t *testing.T) {
			control, memory, contract := ddrProbeMaps(t)
			calls, releases := 0, 0
			v, err := NewHPSContextVerifier(bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) {
				calls++
				t.Fatal("invalid DDR probe reached firmware")
				return [4]uint64{}, nil
			}), ddrContextFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			v.readerMemoryAttempted = true
			v.readerMemoryReady = true
			v.readerMemoryRegion = &Region{Address: 0x95000000, Bytes: readerMemoryPattern(), Release: func() error { releases++; return nil }}
			switch fault {
			case "missing-pin":
				v.readerMemoryRegion = nil
			case "failed-pin":
				v.readerMemoryReady = false
			case "pattern":
				v.readerMemoryRegion.Bytes[8191] ^= 1
			case "address":
				v.readerMemoryRegion.Address += 4096
			case "missing-contract":
				contract = nil
			case "unknown":
				contract = bytes.Replace(contract, []byte(`"format":1`), []byte(`"format":1,"permit":true`), 1)
			case "duplicate":
				contract = bytes.Replace(contract, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
			case "attributes":
				contract = bytes.Replace(contract, []byte(`"arcache":2`), []byte(`"arcache":15`), 1)
			case "close-mask":
				contract = bytes.Replace(contract, []byte(`"close_mask":10`), []byte(`"close_mask":2`), 1)
			case "memory-map":
				memory = bytes.Replace(memory, []byte(`250`), []byte(`251`), 1)
				memory[20] ^= 1
			}
			if got, err := v.ProbeDDRReader(control, memory, contract); err == nil || got != (FPGAIdentityObservation{}) || calls != 0 || releases != 0 {
				t.Fatal(got, err, calls, releases)
			}
		})
	}
}
