package protocol

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/spectrumdisks"
	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
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
	for _, name := range []string{"game.tap", "game.TZX", "game.pzx", "game.csw"} {
		if !AdmitSpectrumTapeName(name) || !AdmitLiveMediaName(name) {
			t.Fatalf("spectrum tape %s refused", name)
		}
	}
	if AdmitSpectrumTapeName("game.p") || AdmitSpectrumTapeName("game.dsk") || AdmitSpectrumTapeName("dir/game.tzx") {
		t.Fatal("spectrum tape names")
	}
}

func TestAtariStFloppyContract(t *testing.T) {
	descriptor := apple2Descriptor(corepackage.Interface{ID: AtariStFloppyInterface().ID, Major: 1, Required: true})
	got := DeclaredCoreMediaCapabilities(descriptor)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"disk","format":"atari-st-floppy","min_bytes":737280,"max_bytes":737280,"interface":{"id":"fes.media.atari-st-floppy","major":1,"minor":0},"transport":"fes-computer-media-unit-v1","unit":0,"extensions":[".st"]}]`
	if string(encoded) != want || !DeclaresDiskMedia(descriptor) {
		t.Fatalf("Atari ST projection %s", encoded)
	}
	for _, name := range []string{"disk.st", "GAME.ST"} {
		if !AdmitAtariStFloppyName(name) || !AdmitComputerDiskName(name) || !AdmitLiveMediaName(name) {
			t.Fatalf("Atari ST name %q rejected", name)
		}
	}
	for _, name := range []string{"", "disk.msa", "disk.stx", "disk.st.gz", "a/b.st", `a\b.st`, "disk.dsk"} {
		if AdmitAtariStFloppyName(name) {
			t.Fatalf("Atari ST name %q accepted", name)
		}
	}
	unit := MediaUnitStatus{Unit: AtariStFloppyUnit, Interface: AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: MediaUnitReady}
	status := computerStatus(unit)
	status.CorePackage.ActiveInterfaces = []RuntimeInterface{{ID: AtariStFloppyInterface().ID, Major: 1}}
	binding := MediaUnitBinding{PackageID: status.CorePackage.PackageID, Generation: 3}
	if !binding.Matches(status) || !binding.AcceptsSize(status, AtariStFloppyBytes) || binding.AcceptsSize(status, AtariStFloppyBytes-1) {
		t.Fatal("exact Atari ST binding rejected or wrong size accepted")
	}
	for _, change := range []func(*MediaUnitStatus){
		func(u *MediaUnitStatus) { u.MinBytes-- }, func(u *MediaUnitStatus) { u.MaxBytes++ },
		func(u *MediaUnitStatus) { u.Unit = 1 }, func(u *MediaUnitStatus) { u.ChunkBytes = 256 },
		func(u *MediaUnitStatus) { u.Interface.Minor = 1 },
	} {
		bad := unit
		change(&bad)
		if bad.Valid() {
			t.Fatalf("invalid Atari ST unit accepted %+v", bad)
		}
	}
}

func TestAtaDiskUnitContractAndPersistentStatus(t *testing.T) {
	noSave := false
	unit := MediaUnitStatus{Unit: AtaDiskUnit, Interface: AtaDiskInterface(), MinBytes: uint32(AtaDiskMinBytes),
		MaxBytes: uint32(AtaDiskMaxBytes), ChunkBytes: AtaDiskChunkBytes, State: MediaUnitEmpty, Saveable: &noSave}
	if !unit.Valid() {
		t.Fatal("empty fixed ATA unit refused")
	}
	p := &CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3,
		ABI:              RuntimeContract{ID: "fes.computer", Major: 1, Minor: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: AtaDiskInterface().ID, Major: AtaDiskInterface().Major}}, MediaUnits: []MediaUnitStatus{unit}}
	if !ComputerABI("fes.computer", 1, 0) || ComputerABI("fes.computer", 1, 1) ||
		!ComputerMediaABI("fes.computer", 1, 1) || ComputerMediaABI("fes.computer", 1, 2) ||
		ComputerMediaABI("fes.computer", 2, 1) || !ComputerAtaDiskABI("fes.computer", 1, 1) ||
		ComputerAtaDiskABI("fes.computer", 1, 0) || ComputerAtaDiskABI("fes.computer", 2, 1) {
		t.Fatal("computer ABI support range is incorrect")
	}
	if got, ok := MediaUnit(p, AtaDiskUnit); !ok || got.State != MediaUnitEmpty || got.CanSave() ||
		!PersistentMediaStatusValid(p) || PersistentMediaBound(p) || MediaDataBound(p) {
		t.Fatalf("empty ATA status = %+v, found=%v", got, ok)
	}
	unit.State = MediaUnitReady
	unit.Persistence = &MediaDataStatus{Mode: "persistent", GameID: "ultima-vi", BaseMediaID: strings.Repeat("c", 64), Revision: "absent"}
	p.MediaUnits[0] = unit
	if !unit.Valid() || !PersistentMediaStatusValid(p) || !PersistentMediaBound(p) || MediaDataBound(p) {
		t.Fatalf("bound ATA status lost its fixed, non-saveable binding: %+v", unit)
	}
	legacy := *p
	legacy.ABI.Minor = 0
	if PersistentMediaStatusValid(&legacy) || PersistentMediaBound(&legacy) {
		t.Fatal("ATA binding was accepted under the 1.0 computer ABI")
	}
	binding := MediaUnitBinding{PackageID: p.PackageID, Generation: p.Generation}
	status := Status{State: StateActive, Development: true, CorePackage: p}
	if binding.Matches(status) || binding.AcceptsSize(status, AtaDiskMinBytes) {
		t.Fatal("fixed ATA disk entered the removable-media operation path")
	}
	for name, change := range map[string]func(*MediaUnitStatus){
		"saveable omitted":      func(u *MediaUnitStatus) { u.Saveable = nil },
		"saveable enabled":      func(u *MediaUnitStatus) { yes := true; u.Saveable = &yes },
		"oversized limit":       func(u *MediaUnitStatus) { u.MaxBytes++ },
		"short chunks":          func(u *MediaUnitStatus) { u.ChunkBytes = 512 },
		"wrong unit":            func(u *MediaUnitStatus) { u.Unit = 1 },
		"wrong interface minor": func(u *MediaUnitStatus) { u.Interface.Minor++ },
	} {
		t.Run(name, func(t *testing.T) {
			bad := unit
			change(&bad)
			if bad.Valid() {
				t.Fatalf("invalid ATA unit accepted: %+v", bad)
			}
		})
	}
	boundOnEmpty := unit
	boundOnEmpty.State = MediaUnitEmpty
	if boundOnEmpty.Valid() {
		t.Fatal("empty ATA unit retained a durable binding")
	}
	readyWithoutBinding := unit
	readyWithoutBinding.Persistence = nil
	if readyWithoutBinding.Valid() {
		t.Fatal("ready ATA disk without a durable binding was accepted")
	}
}

func TestZx81TapeProjection(t *testing.T) {
	descriptor := apple2Descriptor(corepackage.Interface{ID: "fes.media.zx81-tape", Major: 1, Required: true})
	got := DeclaredCoreMediaCapabilities(descriptor)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"cassette","format":"zx81-p","min_bytes":1,"max_bytes":16384,"interface":{"id":"fes.media.zx81-tape","major":1,"minor":0},"transport":"fes-computer-media-unit-v1","unit":0,"extensions":[".p"]}]`
	if string(encoded) != want || !DeclaresZx81Tape(descriptor) || DeclaresDiskMedia(descriptor) {
		t.Fatalf("projection %s", encoded)
	}
	unit := MediaUnitStatus{Unit: Zx81TapeUnit, Interface: Zx81TapeInterface(), MinBytes: 1, MaxBytes: 16384, ChunkBytes: 512, State: MediaUnitReady}
	if !unit.Valid() {
		t.Fatal("exact ZX81 tape unit rejected")
	}
	unit.MaxBytes = 65536
	if unit.Valid() {
		t.Fatal("widened ZX81 tape unit accepted")
	}
	role, ok := ZX81TapeSelectionRole(got)
	if !ok || role != CassetteRole {
		t.Fatalf("tape-unit role = %q ok=%v", role, ok)
	}
	blob := []CoreMediaCapability{{Role: "blob", Format: "raw", Interface: RuntimeContract{ID: "fes.media.blob", Major: 1}}}
	if role, ok = ZX81TapeSelectionRole(blob); !ok || role != "blob" {
		t.Fatalf("legacy blob role = %q ok=%v", role, ok)
	}
	both := append(append([]CoreMediaCapability{}, blob...), got...)
	if role, ok = ZX81TapeSelectionRole(both); !ok || role != CassetteRole {
		t.Fatalf("declared tape unit should win, role=%q ok=%v", role, ok)
	}
	if _, ok = ZX81TapeSelectionRole(DeclaredCoreMediaCapabilities(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true}))); ok {
		t.Fatal("spectrum cassette is not a ZX81 .p role")
	}
}

func TestSpectrumTapeProjection(t *testing.T) {
	got := DeclaredCoreMediaCapabilities(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true}))
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"cassette","format":"spectrum-tape","min_bytes":1,"max_bytes":33554432,"interface":{"id":"fes.media.spectrum-tape","major":2,"minor":0},"transport":"fes-computer-media-unit-v1","unit":0,"extensions":[".tzx",".tap",".pzx",".csw"]}]`
	if string(encoded) != want {
		t.Fatalf("projection %s", encoded)
	}
	// The 64 KiB .tap contract (1.0) is no longer a cassette.
	if got := DeclaredCoreMediaCapabilities(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 1, Required: true})); len(got) != 0 {
		t.Fatalf("1.0 projected %v", got)
	}
	if DeclaresDiskMedia(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true})) ||
		!DeclaresSpectrumTape(apple2Descriptor(corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true})) {
		t.Fatal("tape projected as a disk")
	}
}

func TestSpectrumSnapshotDeclarations(t *testing.T) {
	tape := corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true}
	snapshot := func(id string) corepackage.Interface { return corepackage.Interface{ID: id, Major: 1} }
	base, paged, plus3 := snapshot(SpectrumSnapshotInterfaceID), snapshot(SpectrumSnapshot128InterfaceID), snapshot(SpectrumSnapshotPlus3InterfaceID)
	for name, tc := range map[string]struct {
		interfaces []corepackage.Interface
		runs       [3]bool
	}{
		"tape only":     {[]corepackage.Interface{tape}, [3]bool{}},
		"48K":           {[]corepackage.Interface{tape, base}, [3]bool{true, false, false}},
		"128K":          {[]corepackage.Interface{tape, base, paged}, [3]bool{true, true, false}},
		"+2A/+3":        {[]corepackage.Interface{tape, base, paged, plus3}, [3]bool{true, true, true}},
		"no tape unit":  {[]corepackage.Interface{base, paged, plus3}, [3]bool{}},
		"128K, no base": {[]corepackage.Interface{tape, paged}, [3]bool{}},
	} {
		d := corepackage.Descriptor{Core: corepackage.Core{ID: "example.any-core"},
			ABI: corepackage.Contract{ID: "fes.computer", Major: 1}, Interfaces: tc.interfaces}
		for machine, want := range tc.runs {
			if got := SpectrumSnapshotRuns(d, spectrumsnapshots.Machine(machine)); got != want {
				t.Errorf("%s: class %d runs = %v", name, machine, got)
			}
		}
		media := DeclaredCoreMediaCapabilities(d)
		listed := len(media) == 1 && strings.Contains(strings.Join(media[0].Extensions, " "), ".z80 .sna .szx")
		if listed != tc.runs[0] {
			t.Errorf("%s: snapshot extensions listed = %v (%v)", name, listed, media)
		}
	}
	for name, want := range map[string]bool{"game.z80": true, "GAME.SNA": true, "game.tzx": true, "game.p": false, "a/game.z80": false} {
		if AdmitSpectrumCassetteName(name) != want || AdmitLiveMediaName(name) != (want || name == "game.p") {
			t.Errorf("cassette name %q admitted = %v", name, !want)
		}
	}
}

func TestSpectrumDiskDeclarations(t *testing.T) {
	tape := corepackage.Interface{ID: "fes.media.spectrum-tape", Major: 2, Required: true}
	disk := corepackage.Interface{ID: "fes.media.spectrum-disk", Major: 1, Required: true}
	trdos := corepackage.Interface{ID: SpectrumDiskTrdosInterfaceID, Major: 1}
	plus3 := corepackage.Interface{ID: SpectrumDiskPlus3InterfaceID, Major: 1}
	optional := disk
	optional.Required = false
	for name, tc := range map[string]struct {
		interfaces []corepackage.Interface
		listed     bool
		system     spectrumdisks.System
	}{
		"tape only":           {[]corepackage.Interface{tape}, false, 0},
		"TR-DOS drive":        {[]corepackage.Interface{tape, disk, trdos}, true, spectrumdisks.TRDOS},
		"+3 drive":            {[]corepackage.Interface{tape, disk, plus3}, true, spectrumdisks.Plus3},
		"two controllers":     {[]corepackage.Interface{tape, disk, trdos, plus3}, false, 0},
		"no controller":       {[]corepackage.Interface{tape, disk}, false, 0},
		"no cassette":         {[]corepackage.Interface{disk, trdos}, false, 0},
		"optional disk":       {[]corepackage.Interface{tape, optional, trdos}, false, 0},
		"controller, no unit": {[]corepackage.Interface{tape, plus3}, false, 0},
	} {
		d := corepackage.Descriptor{Core: corepackage.Core{ID: "example.any-core"},
			ABI: corepackage.Contract{ID: "fes.computer", Major: 1}, Interfaces: tc.interfaces}
		if system, ok := SpectrumDiskSystem(d); ok != tc.listed || system != tc.system {
			t.Errorf("%s: disk system %v, %v", name, system, ok)
		}
		var found *CoreMediaCapability
		for _, m := range DeclaredCoreMediaCapabilities(d) {
			if m.Role == DiskRole {
				found = &m
			}
		}
		if (found != nil) != tc.listed {
			t.Fatalf("%s: disk role listed = %v", name, found != nil)
		}
		if found != nil && (*found.Unit != SpectrumDiskUnit || found.Interface != SpectrumDiskInterface() ||
			strings.Join(found.Extensions, " ") != strings.Join(tc.system.Extensions(), " ") ||
			found.MaxBytes != SpectrumDiskImageMaxBytes) {
			t.Errorf("%s: disk capability %+v", name, *found)
		}
	}
	for name, want := range map[string]bool{"game.trd": true, "GAME.SCL": true, "game.dsk": true, "a/game.trd": false} {
		if AdmitSpectrumDiskName(name) != want || AdmitLiveMediaName(name) != want {
			t.Errorf("disk name %q admitted = %v", name, !want)
		}
	}
	unit := MediaUnitStatus{Unit: SpectrumDiskUnit, Interface: SpectrumDiskInterface(),
		MinBytes: uint32(SpectrumDiskMinBytes), MaxBytes: uint32(SpectrumDiskMaxBytes), ChunkBytes: 512, State: MediaUnitReady}
	if !unit.Valid() {
		t.Fatal("observed disk unit refused")
	}
	for _, bad := range []MediaUnitStatus{
		{Unit: 0, Interface: unit.Interface, MinBytes: unit.MinBytes, MaxBytes: unit.MaxBytes, ChunkBytes: 512, State: MediaUnitReady},
		{Unit: 1, Interface: unit.Interface, MinBytes: 1, MaxBytes: unit.MaxBytes, ChunkBytes: 512, State: MediaUnitReady},
	} {
		if bad.Valid() {
			t.Errorf("disk unit %+v admitted", bad)
		}
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

func TestAtariStGeometryRequiresDeclaredAndActiveOptIn(t *testing.T) {
	d := apple2Descriptor(corepackage.Interface{ID: AtariStFloppyInterface().ID, Major: 1, Required: true})
	extension := corepackage.Interface{ID: AtariStFloppyGeometryInterface().ID, Major: 1, Required: true}
	d.Interfaces = append(d.Interfaces, extension)
	projected := DeclaredCoreMediaCapabilities(d)
	if len(projected) != 1 || projected[0].MinBytes != AtariStFloppyGeometryMinBytes || projected[0].MaxBytes != AtariStFloppyGeometryMaxBytes {
		t.Fatal(projected)
	}
	for _, change := range []func(*corepackage.Interface){func(i *corepackage.Interface) { i.Required = false }, func(i *corepackage.Interface) { i.Minor = 1 }, func(i *corepackage.Interface) { i.Major = 2 }} {
		bad := d
		bad.Interfaces = append([]corepackage.Interface(nil), d.Interfaces...)
		change(&bad.Interfaces[len(bad.Interfaces)-1])
		if DeclaredCoreMediaCapabilities(bad)[0].MaxBytes != AtariStFloppyBytes {
			t.Fatal("inexact/optional extension widened declaration")
		}
	}
	u := MediaUnitStatus{Interface: AtariStFloppyInterface(), MinBytes: uint32(AtariStFloppyGeometryMinBytes), MaxBytes: uint32(AtariStFloppyGeometryMaxBytes), ChunkBytes: 512, State: MediaUnitReady}
	s := computerStatus(u)
	s.CorePackage.ActiveInterfaces = []RuntimeInterface{{ID: AtariStFloppyInterface().ID, Major: 1}}
	b := MediaUnitBinding{PackageID: s.CorePackage.PackageID, Generation: 3}
	if b.Matches(s) || b.AcceptsSize(s, 409600) {
		t.Fatal("widened unit admitted without active extension")
	}
	s.CorePackage.ActiveInterfaces = append(s.CorePackage.ActiveInterfaces, RuntimeInterface{ID: AtariStFloppyGeometryInterface().ID, Major: 1})
	if !b.AcceptsSize(s, 409600) || !b.AcceptsSize(s, 839680) || b.AcceptsSize(s, 400000) || b.AcceptsSize(s, 839681) {
		t.Fatal("discrete live geometry admission")
	}
}
