// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// These synthetic DDR identities exercise the reservation contract. They are
// deliberately distinct from the physically observed on-chip package fixture.
func ddrContextFixture(t *testing.T) []byte {
	t.Helper()
	var fields map[string]interface{}
	if err := json.Unmarshal(hpsContextFixture(t), &fields); err != nil {
		t.Fatal(err)
	}
	fields["programming_profile"] = HPSFirstDDRDiagnosticProfile
	fields["control_abi"] = uint32(0x10203041)
	fields["package_id"] = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	fields["build_id"] = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func readerMemoryFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/hps-reader-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readerMemoryCPU(t *testing.T, raw []byte, failure int, calls *int, mutate func(int, *[4]uint64)) FirmwareCaller {
	t.Helper()
	replies := append(hpsContextReplies(sha256.Sum256(raw)), hpsContextReplies(sha256.Sum256(raw))...)
	owner, generation := uint64(17), uint64(1)
	return bridgeFirmwareFunc(func(request [7]uint64) ([4]uint64, error) {
		i := *calls
		*calls++
		if i >= 8 {
			t.Fatal("reader reservation retried firmware")
		}
		want := [7]uint64{0xc2000301, []uint64{0, 4, 5, 0}[i%4], owner, 0x46455342, generation}
		if i%4 == 0 || i%4 == 3 {
			want[2], want[4] = 0, 0
		}
		if request != want {
			t.Fatalf("reader reservation made a mutation: %x", request)
		}
		if i == failure {
			return [4]uint64{}, errors.New("uncertain firmware response")
		}
		r := replies[i]
		if mutate != nil {
			mutate(i, &r)
		}
		if i%4 == 0 {
			owner, generation = r[2], r[3]
		}
		return r, nil
	})
}

func TestReaderMemoryReservationKeepsBootPinnedPatternAndClosedOwner(t *testing.T) {
	raw, mapping := ddrContextFixture(t), readerMemoryFixture(t)
	calls, allocations, releases := 0, 0, 0
	v, err := NewHPSContextVerifier(readerMemoryCPU(t, raw, -1, &calls, nil), raw)
	if err != nil {
		t.Fatal(err)
	}
	var region *Region
	reserve := func(data []byte) (*Region, error) {
		allocations++
		if calls != 4 {
			t.Fatal("reservation preceded cold ownership")
		}
		region = &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}
		data[0] ^= 1 // the allocator cannot change the verifier's reference pattern
		return region, nil
	}
	got, err := v.ReserveReaderMemory(mapping, reserve)
	if err != nil || allocations != 1 || calls != 8 || releases != 0 || v.readerMemoryRegion != region ||
		got.CPUBase != 0x95000000 || got.BusBase != got.CPUBase || got.Bytes != 8192 || got.ArenaBytes != 2<<20 ||
		got.PatternSHA256 != sha256.Sum256(region.Bytes) || got.MapSHA256 != sha256.Sum256(mapping) || !got.Source.Bridges.NeverEnabled {
		t.Fatal(got, err, allocations, calls, releases)
	}
	if _, err := v.ReserveReaderMemory(mapping, reserve); err == nil || calls != 8 || allocations != 1 || releases != 0 {
		t.Fatal("boot-pinned reservation was reused", err)
	}
	if _, ok := interface{}(v).(BootHandoffVerifier); ok {
		t.Fatal("memory reservation granted boot admission")
	}
	if _, err := v.ProbeReader(nil); err == nil || calls != 8 {
		t.Fatal("DDR source used on-chip operation")
	}
}

func TestReaderMemoryReservationRetainsFailedAllocationsAndNeverReplays(t *testing.T) {
	for _, fault := range []string{"allocation-error", "nil", "address", "size", "pattern", "late-bytes", "owner", "generation", "open", "active", "late-owner"} {
		t.Run(fault, func(t *testing.T) {
			raw, mapping := ddrContextFixture(t), readerMemoryFixture(t)
			calls, allocations, releases := 0, 0, 0
			var region *Region
			mutate := func(i int, r *[4]uint64) {
				if i == 0 || i == 3 {
					switch fault {
					case "open":
						r[1] &= ^uint64(2)
					case "active":
						r[1] &= ^uint64(0x40)
					}
				}
				if i == 4 {
					switch fault {
					case "owner":
						r[2]++
					case "generation":
						r[3]++
					case "late-bytes":
						region.Bytes[0] ^= 1
					}
				}
				if i == 7 {
					switch fault {
					case "owner", "late-owner":
						r[2]++
					case "generation":
						r[3]++
					}
				}
			}
			v, err := NewHPSContextVerifier(readerMemoryCPU(t, raw, -1, &calls, mutate), raw)
			if err != nil {
				t.Fatal(err)
			}
			reserve := func(data []byte) (*Region, error) {
				allocations++
				region = &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}
				switch fault {
				case "allocation-error":
					return region, errors.New("allocation uncertain")
				case "nil":
					region = nil
				case "address":
					region.Address++
				case "size":
					region.Bytes = region.Bytes[:4096]
				case "pattern":
					region.Bytes[0] ^= 1
				}
				return region, nil
			}
			got, err := v.ReserveReaderMemory(mapping, reserve)
			if err == nil || got != (ReaderMemoryObservation{}) || releases != 0 || v.readerMemoryRegion != region {
				t.Fatal(got, err, releases)
			}
			if (fault == "open" || fault == "active") && allocations != 0 {
				t.Fatal("unowned source allocated")
			}
			before := calls
			if _, err := v.ReserveReaderMemory(mapping, reserve); err == nil || calls != before || allocations > 1 || releases != 0 {
				t.Fatal("failed reservation replayed", err)
			}
		})
	}
	for failure := 0; failure < 8; failure++ {
		t.Run(string(rune('0'+failure)), func(t *testing.T) {
			raw := ddrContextFixture(t)
			calls, allocations := 0, 0
			v, err := NewHPSContextVerifier(readerMemoryCPU(t, raw, failure, &calls, nil), raw)
			if err != nil {
				t.Fatal(err)
			}
			reserve := func(data []byte) (*Region, error) {
				allocations++
				return &Region{Address: 0x95000000, Bytes: data}, nil
			}
			if got, err := v.ReserveReaderMemory(readerMemoryFixture(t), reserve); err == nil || got != (ReaderMemoryObservation{}) || calls != failure+1 || allocations != failure/4 {
				t.Fatal(got, err, calls, allocations)
			}
			if _, err := v.ReserveReaderMemory(readerMemoryFixture(t), reserve); err == nil || calls != failure+1 {
				t.Fatal("uncertain firmware call replayed")
			}
		})
	}
}

func TestReaderMemoryMapRejectsChangedAddressLifetimeIdentityAndAdmission(t *testing.T) {
	raw, mapping := ddrContextFixture(t), readerMemoryFixture(t)
	calls := 0
	v, err := NewHPSContextVerifier(readerMemoryCPU(t, raw, -1, &calls, nil), raw)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]interface{}{"format": 2, "scope": "trusted", "package_id": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"build_id": "dddddddddddddddddddddddddddddddd", "cpu_base": uint64(0x94000000), "bus_base": 0x15000000, "bytes": 4096,
		"arena_base": uint64(0x94000000), "arena_bytes": 8192, "cpu_attributes": "cached", "lifetime": "until-drained",
		"fpga_addressing": "qualified", "pattern": "zero", "pattern_sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "extra": true} {
		t.Run(key, func(t *testing.T) {
			var fields map[string]interface{}
			if err := json.Unmarshal(mapping, &fields); err != nil {
				t.Fatal(err)
			}
			fields[key] = value
			changed, _ := json.Marshal(fields)
			if _, err := v.ReserveReaderMemory(append(changed, '\n'), func([]byte) (*Region, error) { t.Fatal("invalid map reached allocator"); return nil, nil }); err == nil {
				t.Fatal("changed map accepted")
			}
		})
	}
	for _, changed := range [][]byte{nil, []byte("{}\n"), append(mapping, mapping...), append([]byte(" "), mapping...)} {
		if _, err := v.ReserveReaderMemory(changed, nil); err == nil {
			t.Fatal("malformed map accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid map reached firmware")
	}
	for _, profile := range []string{HPSFirstDiagnosticProfile, HPSFirstDDRDiagnosticProfile} {
		var fields map[string]interface{}
		_ = json.Unmarshal(raw, &fields)
		fields["programming_profile"] = profile
		fields["control_abi"] = uint32(0x10203040)
		if profile == HPSFirstDiagnosticProfile {
			fields["control_abi"] = uint32(0x10203041)
		}
		changed, _ := json.Marshal(fields)
		if _, err := NewHPSContextVerifier(v.cpu, append(changed, '\n')); err == nil {
			t.Fatal("mixed backend and ABI accepted")
		}
	}
	for _, require := range []bool{false, true} {
		p := IdentityProfile{Platform: "de25_nano", Device: "A5EB013BB23BE4SCS", ProgrammingProfile: HPSFirstDDRDiagnosticProfile, ABI: "fes.de25-embedded-diagnostic", Major: 1, ControlABI: 0x10203041, RequireBootHandoff: require}
		if _, err := New(&fake{}, Options{DRAMBase: 0x80000000, ChunkBytes: 64, IdentityProfiles: []IdentityProfile{p}}); err == nil {
			t.Fatal("DDR diagnostic entered runtime admission")
		}
	}
}
