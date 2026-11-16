// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

func windowMemoryFixtures(t *testing.T) (source, control, memory []byte) {
	t.Helper()
	var context map[string]any
	if err := json.Unmarshal(ddrContextFixture(t), &context); err != nil {
		t.Fatal(err)
	}
	context["programming_profile"] = HPSFirstWindowDiagnosticProfile
	context["control_abi"] = scanout.WindowControlABI
	source, _ = json.Marshal(context)
	source = append(source, '\n')
	control, _, _ = ddrProbeMaps(t)
	var fields map[string]any
	if err := json.Unmarshal(control, &fields); err != nil {
		t.Fatal(err)
	}
	fields["fabric_stop"].(map[string]any)["scope"] = "sealed-window-master-stop-v1"
	control, _ = json.Marshal(fields)
	control = append(control, '\n')
	var err error
	memory, err = os.ReadFile("testdata/hps-window-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	return
}
func windowCPUFrame() []byte {
	frame := make([]byte, 3686400)
	for i := range frame {
		frame[i] = byte(i ^ (i >> 8) ^ 0x69)
	}
	return frame
}

func TestWindowMemoryKeepsTheFullArenaAndNeverGrantsData(t *testing.T) {
	source, control, memory := windowMemoryFixtures(t)
	calls, releases := 0, 0
	v, err := NewWindowHPSContextVerifier(readerMemoryCPU(t, source, -1, &calls, nil), source, control, memory)
	if err != nil || calls != 0 {
		t.Fatal(err, calls)
	}
	frame := windowCPUFrame()
	expected := bytes.Clone(frame)
	var region *Region
	got, err := v.ReserveWindowMemory(frame, func(data []byte) (*Region, error) {
		if calls != 4 || len(data) != 4<<20 || !bytes.Equal(data[:len(frame)], frame) || !bytes.Equal(data[len(frame):], make([]byte, (4<<20)-len(frame))) {
			t.Fatal("incomplete or premature window copy")
		}
		region = &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}
		data[0] ^= 1
		return region, nil
	})
	if err != nil || calls != 8 || releases != 0 || got.ArenaBytes != 4<<20 || got.FrameBytes != 3686400 || got.CPUBase != 0x95000000 ||
		got.Geometry != (scanout.WindowGeometry{0x95000000, 4 << 20, 5120, 5120, 720}) || got.CRC32 != crc32.ChecksumIEEE(expected) ||
		got.FrameSHA256 != sha256.Sum256(expected) || got.MapSHA256 != sha256.Sum256(memory) || v.readerMemoryRegion != region {
		t.Fatal(got, err, calls, releases)
	}
	frame[0] ^= 1
	memory[0] ^= 1
	if !bytes.Equal(region.Bytes[:len(frame)], expected) {
		t.Fatal("caller retained the published storage")
	}
	if _, err = v.ReserveWindowMemory(expected, func([]byte) (*Region, error) { t.Fatal("reservation replayed"); return nil, nil }); err == nil || calls != 8 || releases != 0 {
		t.Fatal(err, calls, releases)
	}
	if _, err = v.ReserveReaderMemory(readerMemoryFixture(t), func([]byte) (*Region, error) { t.Fatal("window used fixed-pattern API"); return nil, nil }); err == nil || calls != 8 {
		t.Fatal(err)
	}
	if _, ok := any(v).(BootHandoffVerifier); ok {
		t.Fatal("memory preparation became boot authority")
	}
}

func TestWindowMemoryRetainsEveryFailedReservation(t *testing.T) {
	for _, fault := range []string{"allocation", "nil", "address", "half-arena", "frame", "padding", "late-bytes", "late-owner", "late-generation", "open-before", "firmware-before", "firmware-after"} {
		t.Run(fault, func(t *testing.T) {
			source, control, memory := windowMemoryFixtures(t)
			calls, allocations, releases := 0, 0, 0
			var region *Region
			mutate := func(i int, r *[4]uint64) {
				if fault == "late-bytes" && i == 6 {
					region.Bytes[len(region.Bytes)-1] ^= 1
				}
				if fault == "late-owner" && i == 4 {
					r[2]++
				}
				if fault == "late-generation" && i == 4 {
					r[3]++
				}
				if fault == "open-before" && i == 0 {
					r[1] &^= 1
				}
			}
			failure := -1
			if fault == "firmware-before" {
				failure = 0
			}
			if fault == "firmware-after" {
				failure = 4
			}
			v, err := NewWindowHPSContextVerifier(readerMemoryCPU(t, source, failure, &calls, mutate), source, control, memory)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.ReserveWindowMemory(windowCPUFrame(), func(data []byte) (*Region, error) {
				allocations++
				region = &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }}
				switch fault {
				case "allocation":
					return region, errors.New("allocation uncertain")
				case "nil":
					region = nil
				case "address":
					region.Address += 4096
				case "half-arena":
					region.Bytes = region.Bytes[:2<<20]
				case "frame":
					region.Bytes[0] ^= 1
				case "padding":
					region.Bytes[len(region.Bytes)-1] ^= 1
				}
				return region, nil
			})
			if err == nil || got != (WindowMemoryObservation{}) || releases != 0 || v.readerMemoryRegion != region {
				t.Fatal("failure published authority or lost allocation", got, err, releases)
			}
			before, allocated := calls, allocations
			if _, err = v.ReserveWindowMemory(windowCPUFrame(), func([]byte) (*Region, error) { t.Fatal("uncertain reservation replayed"); return nil, nil }); err == nil || calls != before || allocations != allocated || releases != 0 {
				t.Fatal(err, calls, releases)
			}
		})
	}
}

func TestWindowMemoryRequiresItsExplicitCompiledMaps(t *testing.T) {
	source, control, memory := windowMemoryFixtures(t)
	calls := 0
	cpu := bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) { calls++; return [4]uint64{}, errors.New("unexpected I/O") })
	if _, err := NewHPSContextVerifier(cpu, source); err == nil {
		t.Fatal("ordinary constructor admitted a window")
	}
	var fields map[string]any
	if err := json.Unmarshal(memory, &fields); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		changed := map[string]any{}
		for k, v := range fields {
			changed[k] = v
		}
		switch value.(type) {
		case float64:
			changed[key] = float64(0)
		default:
			changed[key] = "foreign"
		}
		raw, _ := json.Marshal(changed)
		if _, err := NewWindowHPSContextVerifier(cpu, source, control, append(raw, '\n')); err == nil {
			t.Fatal("changed map accepted", key)
		}
	}
	var bank map[string]any
	json.Unmarshal(control, &bank)
	bank["fabric_stop"].(map[string]any)["scope"] = "sealed-ddr-master-stop-v1"
	bad, _ := json.Marshal(bank)
	if _, err := NewWindowHPSContextVerifier(cpu, source, append(bad, '\n'), memory); err == nil {
		t.Fatal("old maintenance bank accepted")
	}
	if calls != 0 {
		t.Fatal("invalid source maps reached firmware", calls)
	}
}
