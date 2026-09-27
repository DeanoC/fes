package misterruntime

import (
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

// The runtime serializer writes these fes.computer statuses; every row must
// pass the strict protocol-2 decoder, including the v2 slot composition.
func TestComputerDecoderConsumesRuntimeSerializerFixtures(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-computer-responses.jsonl")
	if len(lines) != 7 {
		t.Fatalf("computer fixture rows=%d", len(lines))
	}
	for row, line := range lines {
		response, err := decodeProtocol2Response([]byte(line))
		if err != nil {
			t.Fatalf("row %d: %v", row, err)
		}
		units := response.Capabilities.MediaUnits
		switch row {
		case 1, 2, 3, 4:
			want := map[int]string{1: "empty", 2: "ready", 3: "loading", 4: "empty"}[row]
			if len(units) != 1 || units[0].Unit != 0 || units[0].State != want {
				t.Fatalf("row %d media units %+v", row, units)
			}
		default:
			if len(units) != 0 {
				t.Fatalf("row %d media units %+v", row, units)
			}
		}
		if (response.Error != nil) != (row == 3 || row == 6) {
			t.Fatalf("row %d error %+v", row, response.Error)
		}
		var slots *expansion.SlotComposition
		if response.ActivePackage != nil {
			slots = response.ActivePackage.SlotComposition
		}
		if row != 4 {
			if slots != nil {
				t.Fatalf("row %d unexpected slot composition %+v", row, slots)
			}
			continue
		}
		if slots == nil || len(slots.Expansions) != 2 || slots.Expansions[0].Slot != 4 || slots.Expansions[1].Slot != 7 {
			t.Fatalf("slot composition %+v", slots)
		}
		id, err := expansion.SlotCompositionID(slots.PackageID, slots.Expansions, slots.PayloadSHA256)
		if err != nil || id != slots.ID {
			t.Fatalf("slot composition identity %q err=%v, want %q", slots.ID, err, id)
		}
	}
	// A physically absent socket in the tuple is refused.
	bad := strings.Replace(lines[4], `"slot":7,`, `"slot":6,`, 1)
	if _, err := decodeProtocol2Response([]byte(bad)); err == nil {
		t.Fatal("slot 6 composition accepted")
	}
}
