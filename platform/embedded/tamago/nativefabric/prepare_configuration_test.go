// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func canonicalConfiguration(t *testing.T, value interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func configurationSource(t *testing.T) ([]byte, []byte, *Region) {
	t.Helper()
	data := bytes.Repeat([]byte{0x5a}, 128)
	sum, sum384 := sha256.Sum256(data), sha512.Sum384(data)
	var source HPSBootContext
	if err := json.Unmarshal(hpsContextFixture(t), &source); err != nil {
		t.Fatal(err)
	}
	source.CoreSHA256, source.CoreSize = hex.EncodeToString(sum[:]), int64(len(data))
	mapping := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: source.PackageID,
		BuildID: source.BuildID, CoreSHA256: source.CoreSHA256, CoreSHA384: hex.EncodeToString(sum384[:]),
		CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: 0xfffc0,
		ChunkSHA384: []string{hex.EncodeToString(sum384[:])}}
	return canonicalConfiguration(t, source), canonicalConfiguration(t, mapping),
		&Region{Address: mapping.Base, Bytes: data, PublishConfiguration: func(raw []byte) error {
			if len(raw) != ProgramHeaderBytes {
				t.Fatal("invalid publication size")
			}
			return nil
		}, Release: func() error { t.Fatal("preparation released the reservation"); return nil }}
}

func TestColdTransferUsesCompiledBlocksAndRechecksPreparationBeforeStart(t *testing.T) {
	for _, name := range []string{"pass", "map", "source", "address", "changed-data", "changed-owner"} {
		t.Run(name, func(t *testing.T) {
			raw, mapping, region := configurationSource(t)
			var descriptor coldConfigurationMap
			if err := json.Unmarshal(mapping, &descriptor); err != nil {
				t.Fatal(err)
			}
			replies := append(hpsContextReplies(sha256.Sum256(raw)), [4]uint64{0, 0x4643503180000080, 17, 1})
			replies = append(replies, replies[0])
			source := HPSBootObservation{Digest: sha256.Sum256(raw), Bridges: BridgeSnapshot{
				Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: FabricHandshakeAbsent},
				Owner:        17, Generation: 1, ResetMask: 15, NeverEnabled: true, MastersContained: true}}
			calls, starts := 0, 0
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == 0xc2000001 {
					starts++
					return [4]uint64{}, nil
				}
				if calls >= len(replies) {
					t.Fatal("unexpected firmware call", a)
				}
				r := replies[calls]
				calls++
				return r, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "map":
				descriptor.ChunkBytes = 0xffff0 // Advertised maximum is not 64-byte aligned.
				mapping = canonicalConfiguration(t, descriptor)
			case "source":
				source.Digest[0] ^= 1
			case "address":
				region.Address += 64
			}
			transfer, err := v.NewColdConfigurationTransfer(controlMapFixture(t), mapping, region, source)
			if name == "map" || name == "source" || name == "address" {
				if err == nil || transfer != nil || calls != 0 || starts != 0 {
					t.Fatal("invalid constructor reached firmware", err, calls, starts)
				}
				return
			}
			if err != nil || transfer.chunk != descriptor.ChunkBytes || transfer.dramBase != descriptor.DRAMBase {
				t.Fatal("constructor lost compiled transfer geometry", transfer, err)
			}
			if name == "changed-data" {
				region.Bytes[0] ^= 1
			}
			if name == "changed-owner" {
				for _, i := range []int{0, 3, 4, 5} {
					replies[i][2]++
				}
			}
			// Caller-owned maps cannot change the deferred preparation recipe.
			clear(mapping)
			_, _, err = transfer.Advance()
			if name == "pass" {
				if err != nil || starts != 1 || calls != 6 {
					t.Fatal("compiled preparation did not precede START", err, starts, calls)
				}
			} else if err == nil || starts != 0 || !transfer.Status().Failed {
				t.Fatal("changed preparation reached START", err, starts)
			}
		})
	}
}

func TestColdConfigurationPreparationUsesFixedOwnerAndNoAddressRequest(t *testing.T) {
	for _, name := range []string{"pass", "status", "tag", "fixed-source-reply", "publication-unavailable", "publication-error", "size", "owner", "generation", "transport", "after-owner", "after-mask", "after-transport", "preparation-stale-generation"} {
		t.Run(name, func(t *testing.T) {
			raw, mapping, region := configurationSource(t)
			replies := append(hpsContextReplies(sha256.Sum256(raw)), [4]uint64{0, 0x4643503180000080, 17, 1})
			replies = append(replies, replies[0])
			failure, maximum := -1, 5
			switch name {
			case "status":
				replies[4][0] = 5
			case "tag":
				replies[4][1] ^= 1 << 32
			case "fixed-source-reply":
				replies[4][1] &^= programBoundReply
			case "publication-unavailable":
				region.PublishConfiguration = nil
				maximum = 4
			case "publication-error":
				region.PublishConfiguration = func([]byte) error { return errors.New("unknown publication") }
				maximum = 4
			case "size":
				replies[4][1]++
			case "owner":
				replies[4][2]++
			case "generation":
				replies[4][3]++
			case "transport":
				failure = 4
			case "after-owner":
				replies[5][2]++
				maximum = 6
			case "after-mask":
				replies[5][1] &^= 2
				maximum = 6
			case "after-transport":
				failure = 5
				maximum = 6
			case "preparation-stale-generation":
				replies[0][3] = 3
				replies[3] = replies[0]
			}
			calls := 0
			operations := []uint64{0, 4, 5, 0, 8, 0}
			cpu := bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
				i := calls
				calls++
				if i >= len(operations) {
					t.Fatal("preparation retried")
				}
				want := [7]uint64{0xc2000301, operations[i], 17, 0x46455342, replies[0][3]}
				if operations[i] == 0 {
					want[2], want[4] = 0, 0
				}
				if request != want {
					t.Fatal("uncompiled address or mutation entered preparation", request)
				}
				if i == failure {
					return [4]uint64{}, errors.New("lost preparation response")
				}
				return replies[i], nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.PrepareConfiguration(controlMapFixture(t), mapping, region)
			if name == "pass" {
				if err != nil || calls != 6 || observed.Bridges.Generation != 1 || !observed.Bridges.NeverEnabled {
					t.Fatal(observed, err, calls)
				}
				if _, ok := interface{}(v).(BootHandoffVerifier); ok {
					t.Fatal("cold preparation granted live boot admission")
				}
			} else if err == nil || observed != (HPSBootObservation{}) || calls != maximum {
				t.Fatal(observed, err, calls, maximum)
			}
		})
	}
}

func TestConfigurationPreparationRejectsChangedMapBytesAndReservationBeforeSMC(t *testing.T) {
	for _, name := range []string{"control", "empty", "trailing", "unknown", "package", "build", "sha256", "sha384", "bytes", "base", "arena", "dram", "chunk", "chunk-count", "chunk-digest", "region", "region-base", "region-size", "region-data", "release"} {
		t.Run(name, func(t *testing.T) {
			raw, mapping, region := configurationSource(t)
			control := controlMapFixture(t)
			var fields map[string]interface{}
			if err := json.Unmarshal(mapping, &fields); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "control":
				control = nil
			case "empty":
				mapping = nil
			case "trailing":
				mapping = append(mapping, []byte("{}")...)
			case "unknown":
				fields["allow_programming"] = true
			case "package":
				fields["package_id"] = "0" + fields["package_id"].(string)[1:]
			case "build":
				fields["build_id"] = "0" + fields["build_id"].(string)[1:]
			case "sha256":
				fields["core_sha256"] = string(bytes.Repeat([]byte{'f'}, 64))
			case "sha384":
				fields["core_sha384"] = string(bytes.Repeat([]byte{'f'}, 96))
			case "bytes":
				fields["core_size"] = float64(124)
			case "base":
				fields["base"] = float64(0x94000000)
			case "arena":
				fields["size"] = float64(2 << 20)
			case "dram":
				fields["dram_base"] = float64(0)
			case "chunk":
				fields["chunk_bytes"] = float64(63)
			case "chunk-count":
				fields["chunk_sha384"] = []string{}
			case "chunk-digest":
				fields["chunk_sha384"] = []string{string(bytes.Repeat([]byte{'f'}, 96))}
			case "region":
				region = nil
			case "region-base":
				region.Address += 64
			case "region-size":
				region.Bytes = region.Bytes[:124]
			case "region-data":
				region.Bytes[0] ^= 1
			case "release":
				region.Release = nil
			}
			if name != "empty" && name != "trailing" {
				mapping = canonicalConfiguration(t, fields)
			}
			calls := 0
			cpu := bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) {
				calls++
				t.Fatal("invalid staged configuration reached firmware")
				return [4]uint64{}, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			if observed, err := v.PrepareConfiguration(control, mapping, region); err == nil || observed != (HPSBootObservation{}) || calls != 0 {
				t.Fatal(observed, err, calls)
			}
			if observed, err := v.PrepareReconfiguration(control, mapping, region); err == nil || observed != (HPSBootObservation{}) || calls != 0 {
				t.Fatal("reconfiguration accepted invalid staged bytes", observed, err, calls)
			}
		})
	}
}
