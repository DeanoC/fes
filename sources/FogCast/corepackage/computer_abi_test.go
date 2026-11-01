package corepackage

import (
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func TestComputerMinorAcrossExactShellAdmission(t *testing.T) {
	for _, tc := range []struct {
		minor int64
		want  bool
	}{{0, true}, {1, true}, {2, false}, {-1, false}} {
		abi := Contract{ID: "fes.computer", Major: 1, Minor: tc.minor}
		multi := Inspection{Descriptor: Descriptor{ABI: abi, Interfaces: []Interface{{ID: expansion.Apple2Slot, Major: 1}}}}
		_, multiErr := slotCompositionShell(multi, nil)
		single := Inspection{Descriptor: Descriptor{ABI: abi, Interfaces: []Interface{{ID: expansion.Slot, Major: 2}}}}
		_, singleErr := compositionShell(single, nil)
		st := Inspection{Descriptor: Descriptor{Format: 3, Core: Core{ID: "fes.atari-st"}, ROM: &ROM{Role: "firmware"}, ABI: abi,
			Interfaces: []Interface{{ID: expansion.VideoSlot, Major: 1}, {ID: expansion.AtariStSlot, Major: 1}}}}
		_, partsErr := PartsShell(st, nil)
		if SupportedComputerABI(abi) != tc.want || (multiErr == nil) != tc.want || (singleErr == nil) != tc.want || (partsErr == nil) != tc.want {
			t.Fatalf("computer minor %d: multi=%v single=%v parts=%v", tc.minor, multiErr, singleErr, partsErr)
		}
	}
}
