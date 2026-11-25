package protocol

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func pcxtDescriptor() corepackage.Descriptor {
	return corepackage.Descriptor{
		Format: 2,
		Core:   corepackage.Core{ID: "fes.pcxt-dos", Name: "PC/XT", Version: "0.1.0"},
		Target: corepackage.Target{Platform: "de10_nano", Device: "5CSEBA6U23I7", ProgrammingProfile: "fes-gp-v1"},
		ABI:    corepackage.Contract{ID: ComputerABIID, Major: 1, Minor: 1},
		Interfaces: []corepackage.Interface{
			{ID: "fes.video.fixed-720p60", Major: 1, Required: true},
			{ID: "fes.keyboard.hid", Major: 1, Required: true},
			{ID: "fes.audio.pcm-s16-stereo-48k", Major: 1, Required: true},
			{ID: "fes.media.pcxt-floppy", Major: 1, Required: true},
			{ID: PersistenceWordsInterface().ID, Major: 1, Required: true},
			{ID: PcxtMachineInterface().ID, Major: 1, Required: true},
		},
	}
}

func TestPcxtMachineSettingsValidationAndHeaders(t *testing.T) {
	if s := (PcxtMachineSettings{VideoCard: 2, MonoTint: 2}); !s.Valid() || s.String() != "2,2" {
		t.Fatalf("unexpected valid settings: %+v (%q)", s, s.String())
	}
	for _, bad := range []PcxtMachineSettings{{VideoCard: 3}, {MonoTint: 3}} {
		if bad.Valid() {
			t.Errorf("accepted invalid settings %+v", bad)
		}
	}
	if parsed, ok := ParsePcxtMachineSettings("1,2"); !ok || parsed != (PcxtMachineSettings{VideoCard: 1, MonoTint: 2}) {
		t.Fatalf("ParsePcxtMachineSettings(1,2) = %+v, %v", parsed, ok)
	}
	for _, bad := range []string{"", "1", "1,2,3", "01,0", "3,0", "-1,0"} {
		if parsed, ok := ParsePcxtMachineSettings(bad); ok && parsed.Valid() {
			t.Errorf("parsed invalid %q -> %+v", bad, parsed)
		}
	}

	d := pcxtDescriptor()
	if !DeclaresPcxtMachine(d) || !DeclaresPerGameMachine(d) || DeclaresPc386Machine(d) {
		t.Fatal("descriptor classification mismatch")
	}
	d.Interfaces[len(d.Interfaces)-1].Required = false
	if DeclaresPcxtMachine(d) || DeclaresPerGameMachine(d) {
		t.Fatal("optional pcxt machine admitted")
	}

	pkgID, rev := strings.Repeat("a", 64), strings.Repeat("b", 64)
	settings := PcxtMachineSettings{VideoCard: 2, MonoTint: 1}
	layout := PcxtMachineInterface()
	data := CoreData{
		PackageID: pkgID, CoreID: "fes.pcxt-dos", Layout: &layout, Mode: "persistent", Revision: rev,
		GameID: "beast-1984", PcxtMachine: &settings,
	}
	if !data.Valid() {
		t.Fatal("valid CoreData rejected")
	}
	update := CoreSettingsUpdate{ExpectedPackageID: pkgID, ExpectedRevision: "absent", GameID: "beast-1984", PcxtMachine: &settings}
	if !update.Valid() || !update.Applied(data) {
		t.Fatal("valid CoreSettingsUpdate rejected or not applied")
	}
	both := update
	def386 := DefaultMachineSettings()
	both.Machine = &def386
	if both.Valid() {
		t.Fatal("mutually exclusive machine fields accepted")
	}
}
