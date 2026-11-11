package catalog

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestValidateCoreEntryRejectsMalformedPersistedSelections(t *testing.T) {
	good := CoreEntry{GameID: "fpga-valid", Title: "Valid", CoreID: "fes.zx81", PackageID: strings.Repeat("a", 64)}
	if err := ValidateCoreEntry(good); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*CoreEntry){
		"game":             func(e *CoreEntry) { e.GameID = "../bad" },
		"package":          func(e *CoreEntry) { e.PackageID = "bad" },
		"unpaired media":   func(e *CoreEntry) { e.MediaRole = protocol.CassetteRole },
		"unsupported role": func(e *CoreEntry) { e.MediaRole, e.MediaID = "unsupported", strings.Repeat("b", 64) },
		"orphan name":      func(e *CoreEntry) { e.MediaName = "valid.p" },
		"invalid name": func(e *CoreEntry) {
			e.MediaRole, e.MediaID, e.MediaName = protocol.CassetteRole, strings.Repeat("b", 64), "../bad.p"
		},
		"geometry on cassette": func(e *CoreEntry) {
			e.MediaRole, e.MediaID = protocol.CassetteRole, strings.Repeat("b", 64)
			e.DiskGeometry = &protocol.DiskGeometry{Cylinders: 1, Heads: 1, SectorsPerTrack: 1}
		},
		"invalid geometry": func(e *CoreEntry) {
			e.MediaRole, e.MediaID = protocol.HardDiskRole, strings.Repeat("b", 64)
			e.DiskGeometry = &protocol.DiskGeometry{}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entry := good
			mutate(&entry)
			if err := ValidateCoreEntry(entry); err == nil {
				t.Fatal("malformed entry was accepted")
			}
		})
	}
	valid := good
	valid.MediaRole, valid.MediaID, valid.MediaName = protocol.CassetteRole, strings.Repeat("b", 64), "valid.p"
	if err := ValidateCoreEntry(valid); err != nil {
		t.Fatalf("valid cassette: %v", err)
	}
}
