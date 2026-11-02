// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"errors"
	"testing"
)

func failedColdTransfer(t *testing.T, cpu FirmwareCaller) (*HPSContextVerifier, *ConfigurationTransfer, HPSBootObservation) {
	t.Helper()
	raw, _, region := configurationSource(t)
	v, err := NewHPSContextVerifier(cpu, raw)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := NewConfigurationTransfer(cpu, region.Bytes, region, 0x80000000, 0xfffc0, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for step := 0; transfer.Status().Outstanding == 0; step++ {
		if step > 4 {
			t.Fatal("transfer did not issue a buffer")
		}
		if _, _, err = transfer.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	_ = transfer.Cancel(errors.New("original transfer deadline"))
	source := HPSBootObservation{Digest: sha256.Sum256(raw), Bridges: BridgeSnapshot{
		Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: FabricHandshakeAbsent},
		Owner:        17, Generation: 1, ResetMask: 15, NeverEnabled: true, MastersContained: true}}
	return v, transfer, source
}

func TestRetirementNeverReleasesReplaysOrRestoresFailedTransfer(t *testing.T) {
	for _, name := range []string{"pass", "diagnostic-timeout", "diagnostic-error", "snapshot-transport", "snapshot-owner", "snapshot-open", "status", "tag", "owner", "generation", "positive-diagnostic", "retirement-transport"} {
		t.Run(name, func(t *testing.T) {
			snapshots, requests := 0, 0
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == 0xc2000001 {
					return [4]uint64{}, nil
				}
				if a[0] == 0x42000002 {
					return [4]uint64{1}, nil
				}
				if a == ([7]uint64{0xc2000301, 0, 0, 0x46455342}) {
					snapshots++
					if name == "snapshot-transport" {
						return [4]uint64{}, errors.New("snapshot uncertain")
					}
					r := [4]uint64{0, 0x46424f31000000df, 17, 1}
					if name == "snapshot-owner" {
						r[2]++
					}
					if name == "snapshot-open" {
						r[1] &^= 2
					}
					return r, nil
				}
				requests++
				if a != ([7]uint64{0xc2000301, 9, 17, 0x46455342, 1}) {
					t.Fatal("retirement sent address, wrong scope or unexpected operation", a)
				}
				r := [4]uint64{0, 0x4643523100000000, 17, 1}
				switch name {
				case "diagnostic-timeout":
					code := int32(-2047)
					r[1] |= uint64(uint32(code))
				case "diagnostic-error":
					code := int32(-2)
					r[1] |= uint64(uint32(code))
				case "status":
					r[0] = 4
				case "tag":
					r[1] ^= 1 << 32
				case "owner":
					r[2]++
				case "generation":
					r[3]++
				case "positive-diagnostic":
					r[1]++
				case "retirement-transport":
					return [4]uint64{}, errors.New("lost retirement reply")
				}
				return r, nil
			})
			v, transfer, source := failedColdTransfer(t, cpu)
			before := transfer.Status()
			reason := transfer.err
			observed, err := v.RetireConfiguration(transfer, source, configurationMapForRetirement(t))
			success := name == "pass" || name == "diagnostic-timeout" || name == "diagnostic-error"
			if success {
				expected := int32(0)
				if name == "diagnostic-timeout" {
					expected = -2047
				}
				if name == "diagnostic-error" {
					expected = -2
				}
				if err != nil || observed != (ConfigurationRetirement{17, 1, expected}) {
					t.Fatal(observed, err)
				}
			} else if err == nil || observed != (ConfigurationRetirement{}) {
				t.Fatal(observed, err)
			}
			if snapshots != 1 || transfer.Status() != before || transfer.err != reason {
				t.Fatal("diagnostic changed the failed transfer", snapshots, transfer.Status())
			}
			maximum := 1
			if name == "snapshot-transport" || name == "snapshot-owner" || name == "snapshot-open" {
				maximum = 0
			}
			if requests != maximum {
				t.Fatal("unexpected status request", requests)
			}
			if _, err = v.RetireConfiguration(transfer, source, configurationMapForRetirement(t)); err == nil {
				t.Fatal("retirement replayed")
			}
			if _, _, err = transfer.Advance(); err != reason || snapshots != 1 || requests != maximum || transfer.Status() != before {
				t.Fatal("transfer revived or released after retirement", err)
			}
		})
	}
}

func TestRetirementRequiresRetainedFailureAndOriginalHeldSourceBeforeSMC(t *testing.T) {
	for _, name := range []string{"live", "done", "no-pending", "already-attempted", "region", "release", "address", "size", "dram", "chunk", "digest", "source", "owner", "generation", "open", "masters", "fabric"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == 0xc2000001 {
					return [4]uint64{}, nil
				}
				if a[0] == 0x42000002 {
					return [4]uint64{1}, nil
				}
				calls++
				return [4]uint64{}, errors.New("unexpected diagnostic")
			})
			v, transfer, source := failedColdTransfer(t, cpu)
			switch name {
			case "live":
				transfer.err = nil
			case "done":
				transfer.done = true
			case "no-pending":
				transfer.pending = nil
			case "already-attempted":
				transfer.diagnosticAttempted = true
			case "region":
				transfer.region = nil
			case "release":
				transfer.region.Release = nil
			case "address":
				transfer.region.Address += 64
			case "size":
				transfer.region.Bytes = transfer.region.Bytes[:64]
			case "dram":
				transfer.dramBase += 64
			case "chunk":
				transfer.chunk = 64
			case "digest":
				transfer.digest[0] ^= 1
			case "source":
				source.Digest[0] ^= 1
			case "owner":
				source.Bridges.Owner = 0
			case "generation":
				source.Bridges.Generation = 3
			case "open":
				source.Bridges.ResetMask = 13
			case "masters":
				source.Bridges.MastersContained = false
			case "fabric":
				source.Bridges.Capabilities.FabricHandshake = FabricHandshakePresent
			}
			if _, err := v.RetireConfiguration(transfer, source, configurationMapForRetirement(t)); err == nil || calls != 0 {
				t.Fatal("invalid retirement reached firmware", err, calls)
			}
		})
	}
}

func configurationMapForRetirement(t *testing.T) []byte {
	t.Helper()
	_, mapping, _ := configurationSource(t)
	return mapping
}
