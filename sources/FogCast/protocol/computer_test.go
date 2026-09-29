package protocol

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func apple2Descriptor(floppy corepackage.Interface) corepackage.Descriptor {
	return corepackage.Descriptor{
		Core: corepackage.Core{ID: "example.any-core"}, ABI: corepackage.Contract{ID: "fes.computer", Major: 1},
		Interfaces: []corepackage.Interface{{ID: "fes.keyboard.hid", Major: 1, Required: true}, floppy},
	}
}

func TestDiskMediaProjectionFollowsTheFloppyContract(t *testing.T) {
	got := DeclaredCoreMediaCapabilities(apple2Descriptor(corepackage.Interface{ID: "fes.media.apple2-floppy", Major: 1, Required: true}))
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"disk","format":"apple2-dos-order","min_bytes":143360,"max_bytes":143360,"interface":{"id":"fes.media.apple2-floppy","major":1,"minor":0},"transport":"fes-computer-media-unit-v1","unit":0,"extensions":[".dsk",".do"]}]`
	if string(encoded) != want {
		t.Fatalf("projection %s", encoded)
	}
	for name, descriptor := range map[string]corepackage.Descriptor{
		"future floppy": apple2Descriptor(corepackage.Interface{ID: "fes.media.apple2-floppy", Major: 2, Required: true}),
		"floppy minor":  apple2Descriptor(corepackage.Interface{ID: "fes.media.apple2-floppy", Major: 1, Minor: 1, Required: true}),
		"blob on computer": {ABI: corepackage.Contract{ID: "fes.computer", Major: 1},
			Interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}},
		"floppy on simple computer": {ABI: corepackage.Contract{ID: "fes.simple-computer", Major: 1},
			Interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}},
		"future abi": {ABI: corepackage.Contract{ID: "fes.computer", Major: 1, Minor: 1},
			Interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := DeclaredCoreMediaCapabilities(descriptor); len(got) != 0 {
				t.Fatalf("projected %+v", got)
			}
		})
	}
}

func TestDiskMediaNames(t *testing.T) {
	for _, name := range []string{"dos33.dsk", "GAME.DSK", "x.do", "Y.DO"} {
		if !AdmitDiskMediaName(name) || !AdmitLiveMediaName(name) {
			t.Fatalf("%s refused", name)
		}
	}
	for _, name := range []string{"", "prodos.po", "image.nib", "a/b.dsk", `a\b.do`, "tape.p", "disk.dsk.gz"} {
		if AdmitDiskMediaName(name) {
			t.Fatalf("%s accepted", name)
		}
	}
	if !AdmitLiveMediaName("tape.P") || AdmitTapeMediaName("disk.dsk") {
		t.Fatal("tape and disk names confused")
	}
	if !AdmitSpectrumTapeName("game.tap") || !AdmitLiveMediaName("game.TAP") || AdmitSpectrumTapeName("game.tzx") {
		t.Fatal("spectrum tape names")
	}
}

func TestSpectrumTapeProjection(t *testing.T) {
	got := DeclaredCoreMediaCapabilities(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 1, Required: true}))
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"cassette","format":"spectrum-tap","min_bytes":1,"max_bytes":65536,"interface":{"id":"fes.media.spectrum-tape","major":1,"minor":0},"transport":"fes-computer-media-unit-v1","unit":0,"extensions":[".tap"]}]`
	if string(encoded) != want {
		t.Fatalf("projection %s", encoded)
	}
	if DeclaresDiskMedia(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 1, Required: true})) ||
		!DeclaresSpectrumTape(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 1, Required: true})) {
		t.Fatal("tape projected as a disk")
	}
}

func computerStatus(units ...MediaUnitStatus) Status {
	return Status{State: StateActive, Development: true, CorePackage: &CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 3, ABI: RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1}},
		MediaUnits:       units,
	}}
}

func TestMediaUnitBindingRequiresTheObservedUnit(t *testing.T) {
	unit := MediaUnitStatus{Unit: 0, Interface: Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: "empty"}
	status := computerStatus(unit)
	b := MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 3, Target: "dev", TargetID: "kit"}
	if !b.Matches(status) || !b.AcceptsSize(status, 143360) || b.AcceptsSize(status, 143359) || !KeyboardHIDCapable(status.CorePackage) {
		t.Fatal("exact binding refused")
	}
	for name, change := range map[string]func(*Status){
		"no unit":         func(s *Status) { s.CorePackage.MediaUnits = nil },
		"other gen":       func(s *Status) { s.CorePackage.Generation = 4 },
		"inactive floppy": func(s *Status) { s.CorePackage.ActiveInterfaces = s.CorePackage.ActiveInterfaces[:1] },
		"other abi":       func(s *Status) { s.CorePackage.ABI.ID = "fes.simple-computer" },
		"recovery":        func(s *Status) { s.Recovery = RecoveryRebootRequired },
		"bad unit":        func(s *Status) { s.CorePackage.MediaUnits[0].MaxBytes = 1 << 20 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := computerStatus(unit)
			change(&changed)
			if b.Matches(changed) {
				t.Fatal("binding matched")
			}
		})
	}
	headers := http.Header{}
	b.Unit = 0
	b.SetHeaders(headers)
	parsed, ok := MediaUnitHeaders(headers)
	if !ok || parsed != b {
		t.Fatalf("headers %v %+v", headers, parsed)
	}
	for _, bad := range []string{"8", "00", "-1", "x"} {
		headers.Set(MediaUnitHeader, bad)
		if _, ok := MediaUnitHeaders(headers); ok {
			t.Fatalf("unit header %q accepted", bad)
		}
	}
}
