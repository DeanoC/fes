// SPDX-License-Identifier: GPL-3.0-or-later
package sdm

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

const arena = 0x94000000

func TestProbeWireAndOwnership(t *testing.T) {
	buffer := make([]uint32, 8)
	var commands []uint64
	client, err := New(func(request [7]uint64) [4]uint64 {
		want := [7]uint64{MailboxService, request[1], arena + 32, 0, 0, arena, 8}
		if request != want {
			t.Fatalf("wire request %#v", request)
		}
		if !reflect.DeepEqual(buffer, make([]uint32, 8)) {
			t.Fatal("stale response was not cleared")
		}
		commands = append(commands, request[1])
		switch request[1] {
		case configStatus:
			copy(buffer, []uint32{0, 0x10203, 1 << 31, 3, 0, 0})
			return [4]uint64{0, 0, 6}
		case getIDCode:
			buffer[0] = 0x1234567 // Synthetic ID: not a Nano silicon claim.
			return [4]uint64{0, 0, 1}
		default:
			t.Fatalf("unexpected/mutating command %#x", request[1])
			return [4]uint64{}
		}
	}, buffer, arena)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			o, err := client.Probe()
			if err != nil || !o.Configured() || o.IDCode != 0x1234567 || o.Version != 0x10203 {
				t.Errorf("observation %+v, error %v", o, err)
			}
		})
	}
	wg.Wait()
	if len(commands) != 32 {
		t.Fatal(commands)
	}
	for i := 0; i < len(commands); i += 2 {
		if commands[i] != configStatus || commands[i+1] != getIDCode {
			t.Fatal("interleaved probe", commands)
		}
	}
}

func TestProbeFailuresRetireWithoutRetryOrPartialObservation(t *testing.T) {
	for _, stage := range []uint64{configStatus, getIDCode} {
		for name, result := range map[string][4]uint64{
			"busy": {1}, "rejected": {2}, "no response": {3}, "error": {4},
			"unknown status": {99}, "unsupported SMC": {0xffffffff},
			"mailbox error": {0, 1, 6}, "empty response": {},
			"oversized response": {0, 0, 9}, "high count bits": {0, 0, 1<<32 | 6},
			"wrong length": {0, 0, 2},
		} {
			t.Run(fmt.Sprintf("%s/%#x", name, stage), func(t *testing.T) {
				buffer := make([]uint32, 8)
				calls := 0
				client, _ := New(func(request [7]uint64) [4]uint64 {
					calls++
					if request[1] == stage {
						return result
					}
					copy(buffer, []uint32{0, 0, 1 << 31, 3})
					return [4]uint64{0, 0, 6}
				}, buffer, arena)
				o, err := client.Probe()
				if err == nil || o != (Observation{}) {
					t.Fatalf("partial observation escaped: %+v %v", o, err)
				}
				before := calls
				if _, err := client.Probe(); err == nil || calls != before {
					t.Fatal("failed service reused")
				}
			})
		}
	}
}

func TestInvalidIDAndUnconfiguredStates(t *testing.T) {
	for _, id := range []uint32{0, 0xffffffff} {
		buffer := make([]uint32, 8)
		client, _ := New(func(request [7]uint64) [4]uint64 {
			if request[1] == getIDCode {
				buffer[0] = id
				return [4]uint64{0, 0, 1}
			}
			return [4]uint64{0, 0, 6}
		}, buffer, arena)
		if o, err := client.Probe(); err == nil || o != (Observation{}) {
			t.Fatalf("invalid ID %x accepted", id)
		}
	}
	for _, o := range []Observation{
		{State: 0x10000000, Pins: 1 << 31, Soft: 3},
		{State: 0xf0000001, Pins: 1 << 31, Soft: 3},
		{Pins: 0, Soft: 3}, {Pins: 1 << 31, Soft: 1}, {Pins: 1 << 31, Soft: 2},
		{Pins: 1 << 31, Soft: 11},
	} {
		if o.Configured() {
			t.Fatalf("unconfigured observation accepted: %+v", o)
		}
	}
}

func TestInvalidArena(t *testing.T) {
	for _, client := range []*Client{nil, {}} {
		if _, err := client.Probe(); err == nil {
			t.Fatal("uninitialized client accepted")
		}
	}
	invoke := func([7]uint64) [4]uint64 { t.Fatal("invalid client called firmware"); return [4]uint64{} }
	for _, addr := range []uint64{0, 0x80000000, arena + 4, 0xc0000000, ^uint64(0) - 63} {
		if client, err := New(invoke, make([]uint32, 8), addr); err == nil || client != nil {
			t.Fatal(addr)
		}
	}
	if _, err := New(nil, make([]uint32, 8), arena); err == nil {
		t.Fatal("nil transport")
	}
	if _, err := New(invoke, make([]uint32, 7), arena); err == nil {
		t.Fatal("short window")
	}
}

func TestObservationDecodersAndFormatting(t *testing.T) {
	// Include non-zero reserved word 5 (0xf) seen on healthy Agilex 5 hardware.
	obs := Observation{
		IDCode:       0x0361a0dd, // version=0x0, part=0x361a, manufacturer=0x06e (Altera), marker=1
		State:        StateIdle,
		Version:      0x001a0300,
		Pins:         0xc0000089, // nSTATUS (bit 31), nCONFIG (bit 30), MSEL=0x9
		Soft:         (1 << 0) | (1 << 1) | (1 << 2),
		ErrorDetails: 0xf,
	}
	if !obs.Configured() || obs.HasError() {
		t.Fatalf("unexpected status for configured observation: %s", obs.String())
	}
	pins := obs.PinStatus()
	if !pins.NStatus || !pins.NConfig || pins.MSEL != 0x9 || pins.Raw != obs.Pins {
		t.Fatalf("PinStatus=%+v", pins)
	}
	soft := obs.SoftStatus()
	if !soft.ConfDone || !soft.InitDone || !soft.CVPDone || soft.SEUError || soft.Raw != obs.Soft {
		t.Fatalf("SoftStatus=%+v", soft)
	}
	id := obs.IDCodeInfo()
	if !id.Valid || !id.MarkerBit || id.Version != 0x0 || id.PartNumber != 0x361a || id.Manufacturer != 0x06e || id.Raw != obs.IDCode {
		t.Fatalf("IDCodeInfo=%+v", id)
	}
	formatted := obs.String()
	for _, want := range []string{"idcode=0x0361a0dd", "configured=true", "error=false", "state=0x0"} {
		if !containsSubstring(formatted, want) {
			t.Fatalf("String()=%q missing %q", formatted, want)
		}
	}

	inProgress := Observation{State: StateInProgress, Pins: 0xc0000089}
	if inProgress.Configured() || inProgress.HasError() {
		t.Fatalf("StateInProgress configured=%t hasError=%t", inProgress.Configured(), inProgress.HasError())
	}
	coreIOErr := Observation{State: StateErrorCoreIO, Pins: 0xc0000089}
	if coreIOErr.Configured() || !coreIOErr.HasError() {
		t.Fatalf("StateErrorCoreIO configured=%t hasError=%t", coreIOErr.Configured(), coreIOErr.HasError())
	}

	errLoc := Observation{State: StateIdle, Pins: 1 << 31, Soft: 3, ErrorLocation: 0x44}
	if !errLoc.HasError() {
		t.Fatal("expected HasError when ErrorLocation non-zero")
	}

	seu := Observation{State: StateIdle, Pins: 1 << 31, Soft: (1 << 0) | (1 << 1) | (1 << 3)}
	if seu.Configured() || !seu.HasError() || !seu.SoftStatus().SEUError {
		t.Fatalf("SEU status configured=%t hasError=%t soft=%+v", seu.Configured(), seu.HasError(), seu.SoftStatus())
	}

	idleLowNStatus := Observation{State: StateIdle, Pins: 0, Soft: (1 << 0) | (1 << 1)}
	if idleLowNStatus.Configured() || !idleLowNStatus.HasError() {
		t.Fatalf("idle with nSTATUS low configured=%t hasError=%t", idleLowNStatus.Configured(), idleLowNStatus.HasError())
	}

	for _, badID := range []uint32{0, 0xffffffff, 0x0361a0dc} {
		if info := (Observation{IDCode: badID}).IDCodeInfo(); info.Valid {
			t.Fatalf("expected invalid IDCodeInfo for %#x: %+v", badID, info)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
