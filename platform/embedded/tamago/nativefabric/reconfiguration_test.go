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

func reconfigurationReplies(raw []byte) [][4]uint64 {
	replies := hpsContextReplies(sha256.Sum256(raw))
	for _, i := range []int{0, 3} {
		replies[i][1] &^= 64 // NeverEnabled is permanently cleared by owned control opening.
		replies[i][3] = 3
	}
	replies = append(replies, [4]uint64{0, 0x4657523100000080, 17, 3}, replies[0])
	return replies
}

func TestReconfigurationPreparationBindsNewClosedGeneration(t *testing.T) {
	for _, name := range []string{"pass", "cold", "initial-generation", "exhausted-generation", "context", "reset", "masters", "status", "tag", "size", "owner", "generation", "transport", "after-owner", "after-generation", "after-reset", "after-transport"} {
		t.Run(name, func(t *testing.T) {
			raw, mapping, region := configurationSource(t)
			replies := reconfigurationReplies(raw)
			failure, maximum := -1, 5
			switch name {
			case "pass":
				maximum = 6
			case "cold":
				replies[0][1] |= 64
				replies[3] = replies[0]
				maximum = 4
			case "initial-generation", "exhausted-generation":
				replies[0][3] = 1
				if name == "exhausted-generation" {
					replies[0][3] = ^uint64(0) - 1
				}
				replies[3] = replies[0]
				maximum = 4
			case "context":
				replies[1][2] ^= 1
				maximum = 4
			case "reset", "masters":
				if name == "reset" {
					replies[0][1] &^= 2
				} else {
					replies[0][1] &^= 128
				}
				maximum = 1
			case "status":
				replies[4][0] = 4
			case "tag":
				replies[4][1] = 0x4643503100000080 // A cold receipt cannot acknowledge rearming.
			case "size":
				replies[4][1]++
			case "owner":
				replies[4][2]++
			case "generation":
				replies[4][3]++
			case "transport":
				failure = 4
			case "after-owner", "after-generation", "after-reset", "after-transport":
				maximum = 6
				switch name {
				case "after-owner":
					replies[5][2]++
				case "after-generation":
					replies[5][3]++
				case "after-reset":
					replies[5][1] &^= 2
				case "after-transport":
					failure = 5
				}
			}
			calls := 0
			operations := []uint64{0, 4, 5, 0, 11, 0}
			cpu := bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
				i := calls
				calls++
				if i >= len(operations) {
					t.Fatal("reconfiguration preparation replayed")
				}
				want := [7]uint64{0xc2000301, operations[i], 17, 0x46455342, replies[0][3]}
				if operations[i] == 0 {
					want[2], want[4] = 0, 0
				}
				if request != want {
					t.Fatal("rearm sent an address, digest or wrong scope", request, want)
				}
				if i == failure {
					return [4]uint64{}, errors.New("unknown rearm outcome")
				}
				return replies[i], nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := v.PrepareReconfiguration(controlMapFixture(t), mapping, region)
			if name == "pass" {
				if err != nil || observed.Bridges.Generation != 3 || observed.Bridges.NeverEnabled {
					t.Fatal(observed, err)
				}
			} else if err == nil || observed != (HPSBootObservation{}) {
				t.Fatal("invalid rearm accepted", observed, err)
			}
			if calls != maximum {
				t.Fatal("wrong rejection boundary", calls, maximum)
			}
		})
	}
}

func TestReconfigurationTransferRetainsFailedLeaseAndRechecksBeforeStart(t *testing.T) {
	for _, name := range []string{"pass", "changed-data", "changed-source", "write", "missing", "done", "release"} {
		t.Run(name, func(t *testing.T) {
			raw, mapping, region := configurationSource(t)
			var descriptor coldConfigurationMap
			if err := json.Unmarshal(mapping, &descriptor); err != nil {
				t.Fatal(err)
			}
			descriptor.ChunkBytes = 64
			sum := sha512.Sum384(region.Bytes[:64])
			descriptor.ChunkSHA384 = []string{hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:])}
			mapping = canonicalConfiguration(t, descriptor)
			wire := &transferWire{t: t, region: region, source: bytes.Clone(region.Bytes), fault: name}
			region.Release = func() error {
				wire.releases++
				if name == "release" {
					return errors.New("uncertain region release")
				}
				return nil
			}
			replies := reconfigurationReplies(raw)
			calls := 0
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] != 0xc2000301 {
					if calls != 6 {
						t.Fatal("transfer preceded complete rearm", calls)
					}
					return wire.Invoke(a)
				}
				if calls >= len(replies) {
					t.Fatal("rearm replayed")
				}
				r := replies[calls]
				calls++
				return r, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			source := HPSBootObservation{Digest: sha256.Sum256(raw), Bridges: BridgeSnapshot{
				Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: FabricHandshakeAbsent},
				Owner:        17, Generation: 3, ResetMask: 15, MastersContained: true}}
			if cold, err := v.NewColdConfigurationTransfer(controlMapFixture(t), mapping, region, source); err == nil || cold != nil {
				t.Fatal("cold constructor accepted a returned source")
			}
			transfer, err := v.NewReconfigurationTransfer(controlMapFixture(t), mapping, region, source)
			if err != nil || calls != 0 || len(wire.calls) != 0 {
				t.Fatal("constructor issued configuration", err, calls)
			}
			clear(mapping) // Deferred preparation owns its recipe.
			if name == "changed-data" {
				region.Bytes[0] ^= 1
			}
			if name == "changed-source" {
				for _, i := range []int{0, 3, 4, 5} {
					replies[i][2]++
				}
			}
			for steps := 0; steps < 30 && !transfer.Status().Done && !transfer.Status().Failed; steps++ {
				_, _, err = transfer.Advance()
			}
			if name == "pass" {
				if err != nil || !transfer.Status().Done || wire.releases != 1 || !bytes.Equal(wire.received, wire.source) {
					t.Fatal("reconfiguration incomplete", transfer.Status(), err, wire.releases)
				}
			} else {
				if err == nil || !transfer.Status().Failed || transfer.Status().RetainedBytes != 128 || (wire.releases != 0 && name != "release") {
					t.Fatal("failed transfer lost its lease", transfer.Status(), err, wire.releases)
				}
				if (name == "changed-data" || name == "changed-source") && len(wire.calls) != 0 {
					t.Fatal("changed source reached START")
				}
			}
			oldCalls, oldWireCalls, oldReleases := calls, len(wire.calls), wire.releases
			_, _, repeated := transfer.Advance()
			if repeated != err || calls != oldCalls || len(wire.calls) != oldWireCalls || wire.releases != oldReleases {
				t.Fatal("terminal transfer replayed or released again")
			}
		})
	}
}
