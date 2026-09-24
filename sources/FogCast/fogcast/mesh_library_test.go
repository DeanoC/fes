package fogcast

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func TestProjectMeshLibraryRepresentativeTitles(t *testing.T) {
	bios := strings.Repeat("11", 32)
	cart := strings.Repeat("22", 32)
	ram := strings.Repeat("33", 32)
	snesROM := strings.Repeat("44", 32)
	colecoPkg := strings.Repeat("ab", 32)
	zxPkg := strings.Repeat("cd", 32)

	coleco := meshCoreTitle("Frogger", "fes.coleco", colecoPkg, cart, true)
	coleco.ABI = corepackage.Contract{ID: "fes.application", Major: 1, Minor: 0}
	zx := meshCoreTitle("3D Monster Maze", "fes.zx81", zxPkg, "", false)
	zx.ABI = corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 0}
	zx.Expansions = []MeshExpansion{{Name: expansion.Slot, Digest: ram}}
	native := meshNativeTitle("Super Mario World", protocol.SystemSNES, snesROM)
	missingBIOS := meshCoreTitle("Donkey Kong", "fes.coleco", colecoPkg, cart, true)
	uppercase := meshCoreTitle("Frogger", "fes.coleco", colecoPkg, cart, true)
	development := meshCoreTitle("Probe", "fes.coleco", colecoPkg, cart, false)
	development.Execute = ExecutionFPGADevelopment

	filled := catalog.CoreFirmware{Slot: protocol.FirmwareRole, MediaID: bios, Size: protocol.FirmwareBytes}
	cases := []struct {
		name       string
		lib        MeshLibrary
		skipReason string
		check      func(t *testing.T, entry meshcontent.Entry)
	}{
		{
			name: "coleco fpga bios and cart",
			lib:  MeshLibrary{Firmware: filled, Titles: []MeshTitle{coleco}},
			check: func(t *testing.T, entry meshcontent.Entry) {
				if entry.System != "coleco" || !entry.Launchable || len(entry.Execute) != 1 || entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
					t.Fatalf("coleco shape %+v", entry)
				}
				if err := protocol.ValidateGameID(entry.TitleID); err != nil {
					t.Fatal(err)
				}
				want := []string{meshcontent.SlotPackageABI, meshcontent.SlotBIOS, meshcontent.SlotPrimaryMedia}
				if len(entry.Slots) != len(want) {
					t.Fatalf("slots %+v", entry.Slots)
				}
				for i, kind := range want {
					if entry.Slots[i].Kind != kind {
						t.Fatalf("slot %d = %s", i, entry.Slots[i].Kind)
					}
				}
				pkg := entry.Slots[0].Package
				if pkg == nil || pkg.PackageID != colecoPkg || pkg.ABI != "fes.application" || pkg.Major != 1 {
					t.Fatalf("package abi %+v", pkg)
				}
				if entry.Slots[1].Content.Digest != bios || entry.Slots[1].Content.Algorithm != meshcontent.AlgorithmSHA256 {
					t.Fatalf("bios content %+v", entry.Slots[1].Content)
				}
				if entry.Slots[2].Content.Digest != cart {
					t.Fatalf("cart content %+v", entry.Slots[2].Content)
				}
				assertNoPath(t, entry)
			},
		},
		{
			name: "zx81 expansion",
			lib:  MeshLibrary{Firmware: filled, Titles: []MeshTitle{zx}},
			check: func(t *testing.T, entry meshcontent.Entry) {
				if entry.System != "zx81" || len(entry.Execute) != 1 || entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
					t.Fatalf("zx shape %+v", entry)
				}
				if len(entry.Slots) != 2 || entry.Slots[0].Kind != meshcontent.SlotPackageABI || entry.Slots[1].Kind != meshcontent.SlotExpansion {
					t.Fatalf("slots %+v", entry.Slots)
				}
				if entry.Slots[0].Package.PackageID != zxPkg || entry.Slots[0].Package.ABI != "fes.simple-computer" || entry.Slots[0].Package.Major != 1 {
					t.Fatalf("package %+v", entry.Slots[0].Package)
				}
				if entry.Slots[1].Name != expansion.Slot || entry.Slots[1].Content.Digest != ram {
					t.Fatalf("expansion %+v", entry.Slots[1])
				}
				for _, slot := range entry.Slots {
					if slot.Kind == meshcontent.SlotBIOS {
						t.Fatal("zx81 took the household bios")
					}
				}
				assertNoPath(t, entry)
			},
		},
		{
			name: "native emu",
			lib:  MeshLibrary{Firmware: filled, Titles: []MeshTitle{native}},
			check: func(t *testing.T, entry meshcontent.Entry) {
				if entry.System != string(protocol.SystemSNES) || len(entry.Execute) != 1 || entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu {
					t.Fatalf("native shape %+v", entry)
				}
				if len(entry.Slots) != 1 || entry.Slots[0].Kind != meshcontent.SlotPrimaryMedia || entry.Slots[0].Content.Digest != snesROM {
					t.Fatalf("slots %+v", entry.Slots)
				}
				if entry.Slots[0].Package != nil {
					t.Fatal("native_emu gained a package slot")
				}
				assertNoPath(t, entry)
			},
		},
		{
			name:       "missing household firmware",
			lib:        MeshLibrary{Titles: []MeshTitle{missingBIOS}},
			skipReason: "household firmware digest is required",
		},
		{
			name:       "uppercase stored digest is not rewritten",
			lib:        MeshLibrary{Firmware: catalog.CoreFirmware{Slot: protocol.FirmwareRole, MediaID: strings.ToUpper(strings.Repeat("ab", 32))}, Titles: []MeshTitle{uppercase}},
			skipReason: "household firmware digest is not a stored sha256",
		},
		{
			name:       "development execution is not relabeled",
			lib:        MeshLibrary{Firmware: filled, Titles: []MeshTitle{development}},
			skipReason: "execution is not a mesh catalog kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, skipped := ProjectMeshLibrary(tc.lib)
			if tc.skipReason != "" {
				if len(entries) != 0 || len(skipped) != 1 || skipped[0].Reason != tc.skipReason {
					t.Fatalf("entries %+v skipped %+v", entries, skipped)
				}
				if strings.Contains(skipped[0].Reason, "sha256:") {
					t.Fatalf("skip fabricated a content-id: %+v", skipped[0])
				}
				return
			}
			if len(skipped) != 0 || len(entries) != 1 {
				t.Fatalf("entries %d skipped %+v", len(entries), skipped)
			}
			if err := entries[0].Validate(); err != nil {
				t.Fatal(err)
			}
			tc.check(t, entries[0])
		})
	}
}

func TestProjectMeshLibrarySG1000BrowseSystem(t *testing.T) {
	pkg := strings.Repeat("ef", 32)
	cart := strings.Repeat("55", 32)
	title := meshCoreTitle("SG-1000", "fes.sg1000", pkg, cart, false)
	title.ABI = corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 0}
	if title.Game.System != catalog.CorePlatform {
		t.Fatalf("storage platform %s", title.Game.System)
	}
	entries, skipped := ProjectMeshLibrary(MeshLibrary{Titles: []MeshTitle{title}})
	if len(skipped) != 0 || len(entries) != 1 {
		t.Fatalf("entries %+v skipped %+v", entries, skipped)
	}
	entry := entries[0]
	if entry.System != "sg1000" {
		t.Fatalf("browse system %q", entry.System)
	}
	if !entry.Launchable || len(entry.Execute) != 1 || entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
		t.Fatalf("shape %+v", entry)
	}
	if len(entry.Slots) != 2 || entry.Slots[0].Kind != meshcontent.SlotPackageABI || entry.Slots[1].Kind != meshcontent.SlotPrimaryMedia {
		t.Fatalf("slots %+v", entry.Slots)
	}
	if entry.Slots[0].Package == nil || entry.Slots[0].Package.PackageID != pkg || entry.Slots[0].Package.ABI != "fes.simple-computer" || entry.Slots[0].Package.Major != 1 {
		t.Fatalf("package %+v", entry.Slots[0].Package)
	}
	if entry.Slots[1].Content == nil || entry.Slots[1].Content.Digest != cart || entry.Slots[1].Content.Algorithm != meshcontent.AlgorithmSHA256 {
		t.Fatalf("cart %+v", entry.Slots[1].Content)
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	assertNoPath(t, entry)
}

func TestProjectMeshLibraryMultiExpansionOrder(t *testing.T) {
	pkg := strings.Repeat("cd", 32)
	bus := strings.Repeat("66", 32)
	printer := strings.Repeat("77", 32)
	title := meshCoreTitle("3D Monster Maze", "fes.zx81", pkg, "", false)
	title.ABI = corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 0}
	title.Expansions = []MeshExpansion{
		{Name: expansion.Slot, Digest: bus},
		{Name: "fes.expansion.printer", Digest: printer},
	}
	entries, skipped := ProjectMeshLibrary(MeshLibrary{Titles: []MeshTitle{title}})
	if len(skipped) != 0 || len(entries) != 1 {
		t.Fatalf("entries %+v skipped %+v", entries, skipped)
	}
	entry := entries[0]
	if entry.System != "zx81" || len(entry.Slots) != 3 {
		t.Fatalf("shape %+v", entry)
	}
	if entry.Slots[0].Kind != meshcontent.SlotPackageABI {
		t.Fatalf("package slot %+v", entry.Slots[0])
	}
	want := title.Expansions
	for i, expansionSlot := range want {
		slot := entry.Slots[i+1]
		if slot.Kind != meshcontent.SlotExpansion || slot.Name != expansionSlot.Name {
			t.Fatalf("expansion %d %+v", i, slot)
		}
		if slot.Content == nil || slot.Content.Algorithm != meshcontent.AlgorithmSHA256 || slot.Content.Digest != expansionSlot.Digest {
			t.Fatalf("expansion %d content %+v", i, slot.Content)
		}
	}
	ids := entry.ContentIDs()
	if len(ids) != 2 || ids[0].String() == ids[1].String() || ids[0].Digest != bus || ids[1].Digest != printer {
		t.Fatalf("content-ids %+v", ids)
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	assertNoPath(t, entry)
}

func TestProjectMeshLibraryROMLessPong(t *testing.T) {
	pkg := strings.Repeat("ab", 32)
	title := meshCoreTitle("Pong", "fes.pong", pkg, "", false)
	title.ABI = corepackage.Contract{ID: "fes.simple-game", Major: 1, Minor: 0}
	if title.Game.System != catalog.CorePlatform || title.Core.MediaID != "" || title.Core.FirmwareRequired {
		t.Fatalf("pong fixture %+v core %+v", title.Game, title.Core)
	}
	entries, skipped := ProjectMeshLibrary(MeshLibrary{
		Firmware: catalog.CoreFirmware{Slot: protocol.FirmwareRole, MediaID: strings.Repeat("11", 32)},
		Titles:   []MeshTitle{title},
	})
	if len(skipped) != 0 || len(entries) != 1 {
		t.Fatalf("entries %+v skipped %+v", entries, skipped)
	}
	entry := entries[0]
	if entry.System != "pong" || !entry.Launchable || len(entry.Execute) != 1 || entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
		t.Fatalf("shape %+v", entry)
	}
	if len(entry.Slots) != 1 || entry.Slots[0].Kind != meshcontent.SlotPackageABI || entry.Slots[0].Content != nil {
		t.Fatalf("slots %+v", entry.Slots)
	}
	pkgSlot := entry.Slots[0].Package
	if pkgSlot == nil || pkgSlot.PackageID != pkg || pkgSlot.ABI != "fes.simple-game" || pkgSlot.Major != 1 {
		t.Fatalf("package %+v", pkgSlot)
	}
	if len(entry.ContentIDs()) != 0 {
		t.Fatalf("content slots %+v", entry.ContentIDs())
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	assertNoPath(t, entry)
}

func TestProjectMeshLibrarySkipsOneTitleAndKeepsTheRest(t *testing.T) {
	bios := strings.Repeat("11", 32)
	cart := strings.Repeat("22", 32)
	ram := strings.Repeat("33", 32)
	snesROM := strings.Repeat("44", 32)
	coleco := meshCoreTitle("Frogger", "fes.coleco", strings.Repeat("ab", 32), cart, true)
	zx := meshCoreTitle("Monster Maze", "fes.zx81", strings.Repeat("cd", 32), "", false)
	zx.Expansions = []MeshExpansion{{Name: expansion.Slot, Digest: ram}}
	native := meshNativeTitle("Super Mario World", protocol.SystemSNES, snesROM)
	broken := meshCoreTitle("Untitled", "fes.pong", "", "", false)
	broken.Core.PackageID = ""
	lib := MeshLibrary{
		Firmware: catalog.CoreFirmware{Slot: protocol.FirmwareRole, MediaID: bios},
		Titles:   []MeshTitle{coleco, broken, zx, native},
	}
	entries, skipped := ProjectMeshLibrary(lib)
	if len(entries) != 3 || len(skipped) != 1 || skipped[0].TitleID != broken.Game.ID || skipped[0].Reason != "package abi is required" {
		t.Fatalf("entries %+v skipped %+v", entries, skipped)
	}
	if entries[0].System != "coleco" || entries[1].System != "zx81" || entries[2].Execute[0].Kind != meshcontent.ExecuteNativeEmu {
		t.Fatalf("order %+v", entries)
	}
	if entries[0].Slots[1].Content.Digest != bios || entries[1].Slots[1].Content.Digest != ram || entries[2].Slots[0].Content.Digest != snesROM {
		t.Fatalf("digests changed %+v", entries)
	}
}

func meshCoreTitle(title, coreID, packageID, mediaID string, firmware bool) MeshTitle {
	gameID := catalog.GameID(catalog.CorePlatform, "core-packages", coreID+"\x00"+title, title)
	game := catalog.Game{
		ID: gameID, Title: title, System: catalog.CorePlatform,
		Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true,
		RelativePath: "logical/" + coreID,
	}
	entry := &catalog.CoreEntry{
		GameID: gameID, Title: title, CoreID: coreID, PackageID: packageID, FirmwareRequired: firmware,
	}
	if mediaID != "" {
		entry.MediaRole = "blob"
		entry.MediaID = mediaID
	}
	return MeshTitle{
		Game: game, Launchable: true, Execute: ExecutionFPGANative, Core: entry,
		ABI: corepackage.Contract{ID: "fes.application", Major: 1},
	}
}

func meshNativeTitle(title string, system protocol.System, digest string) MeshTitle {
	gameID := catalog.GameID(system, "household", "roms/"+title+".sfc", title)
	return MeshTitle{
		Game: catalog.Game{
			ID: gameID, Title: title, System: system, Kind: catalog.SourceKindRaw,
			State: catalog.SourceStateAvailable, RootOnline: true,
			RelativePath: "roms/" + title + ".sfc",
			Content:      &catalog.Content{SHA256: digest, Size: 32, Extension: "sfc"},
		},
		Launchable: true,
		Execute:    ExecutionHostOnly,
	}
}

func assertNoPath(t *testing.T, entry meshcontent.Entry) {
	t.Helper()
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, absent := range []string{"/", "logical", ".sfc", "removable", "secondary", "path"} {
		if strings.Contains(body, absent) {
			t.Fatalf("entry carried %q: %s", absent, body)
		}
	}
}
