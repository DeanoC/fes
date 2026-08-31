package core_test

import (
	"testing"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/protocol"
)

func TestRegistry(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	mega, ok := registry.Lookup(protocol.SystemMegaDrive)
	if !ok || mega.ExpectedCore != "MegaDrive" || mega.RBFSelector != "_Console/MegaDrive" || mega.ROMRoot != "/media/fat/games/MegaDrive" || mega.MGLRoot != "/media/fat/games/MegaDrive" || mega.FileIndex != 1 || mega.FileDelay != 1 || mega.FileType != "f" {
		t.Fatalf("unexpected Mega Drive spec: %#v, ok=%v", mega, ok)
	}
	snes, ok := registry.Lookup(protocol.SystemSNES)
	if !ok || snes.ExpectedCore != "SNES" || snes.RBFSelector != "_Console/SNES" || snes.ROMRoot != "/media/fat/games/SNES" || snes.MGLRoot != "/media/fat/games/SNES" || snes.FileIndex != 0 || snes.FileDelay != 2 || snes.FileType != "f" {
		t.Fatalf("unexpected SNES spec: %#v, ok=%v", snes, ok)
	}
	for _, test := range []struct {
		system                      protocol.System
		extension                   string
		core, rbf, romRoot, mglRoot string
		delay, index                int
	}{
		{protocol.SystemNES, ".nes", "NES", "_Console/NES", "/media/fat/games/NES", "/media/fat/games/NES", 1, 0},
		{protocol.SystemSMS, ".sms", "SMS", "_Console/SMS", "/media/fat/games/SMS", "/media/fat/games/SMS", 1, 1},
		{protocol.SystemGameBoy, ".gb", "GAMEBOY", "_Console/Gameboy", "/media/fat/games/Gameboy", "/media/fat/games/Gameboy", 2, 1},
		{protocol.SystemGBA, ".gba", "GBA", "_Console/GBA", "/media/fat/games/GBA", "/media/fat/games/GBA", 2, 0},
		{protocol.SystemPCE, ".pce", "TGFX16", "_Console/TurboGrafx16", "/media/fat/games/TGFX16", "/media/fat/games/TGFX16", 1, 0},
		{protocol.SystemGameGear, ".gg", "SMS", "_Console/SMS", "/media/fat/games/SMS", "/media/fat/games/SMS", 1, 2},
		{protocol.SystemGameBoyColor, ".gbc", "GAMEBOY", "_Console/Gameboy", "/media/fat/games/Gameboy", "/media/fat/games/Gameboy", 2, 1},
		{protocol.SystemAtari2600, ".a26", "ATARI7800", "_Console/Atari7800", "/media/fat/games/Atari2600", "/media/fat/games/ATARI7800", 1, 1},
		{protocol.SystemAtari7800, ".a78", "ATARI7800", "_Console/Atari7800", "/media/fat/games/ATARI7800", "/media/fat/games/ATARI7800", 1, 1},
		{protocol.SystemColecoVision, ".col", "Coleco", "_Console/ColecoVision", "/media/fat/games/Coleco", "/media/fat/games/Coleco", 1, 0},
		{protocol.SystemAtariLynx, ".lnx", "AtariLynx", "_Console/AtariLynx", "/media/fat/games/AtariLynx", "/media/fat/games/AtariLynx", 1, 0},
		{protocol.SystemWonderSwan, ".ws", "WonderSwan", "_Console/WonderSwan", "/media/fat/games/WonderSwan", "/media/fat/games/WonderSwan", 1, 1},
		{protocol.SystemWonderSwanColor, ".wsc", "WonderSwan", "_Console/WonderSwan", "/media/fat/games/WonderSwanColor", "/media/fat/games/WonderSwan", 1, 1},
		{protocol.SystemIntellivision, ".int", "Intellivision", "_Console/Intellivision", "/media/fat/games/Intellivision", "/media/fat/games/Intellivision", 1, 0},
	} {
		spec, ok := registry.Lookup(test.system)
		if !ok || spec.System != test.system || spec.ExpectedCore != test.core || spec.RBFSelector != test.rbf || spec.ROMRoot != test.romRoot || spec.MGLRoot != test.mglRoot || spec.FileDelay != test.delay || spec.FileType != "f" || spec.FileIndex != test.index {
			t.Fatalf("unexpected %s spec: %#v, ok=%v", test.system, spec, ok)
		}
		if _, ok := spec.Extensions[test.extension]; !ok {
			t.Fatalf("%s extensions = %#v, missing %s", test.system, spec.Extensions, test.extension)
		}
	}
	lynx, ok := registry.Lookup(protocol.SystemAtariLynx)
	if !ok || len(lynx.RequiredFiles) != 1 || lynx.RequiredFiles[0] != "boot.rom" {
		t.Fatalf("Lynx registry prerequisites = %#v, %v", lynx.RequiredFiles, ok)
	}
	a2600, ok := registry.Lookup(protocol.SystemAtari2600)
	if !ok || !a2600.RequiresLaunchIntent {
		t.Fatalf("Atari 2600 intent requirement = %#v, %v", a2600, ok)
	}
}

func TestLookupObserved(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	spec, ok := registry.LookupObserved("MegaDrive")
	if !ok || spec.System != protocol.SystemMegaDrive {
		t.Fatalf("LookupObserved(MegaDrive) = %#v, %v", spec, ok)
	}
	if _, ok := registry.LookupObserved("Genesis"); ok {
		t.Fatal("deprecated Genesis core must not match the POC registry")
	}
	sms, ok := registry.LookupObserved("SMS")
	if !ok || sms.System != protocol.SystemSMS {
		t.Fatalf("LookupObserved(SMS) = %#v, %v; canonical SMS fallback must remain available without launch intent", sms, ok)
	}
	if !registry.RecognizesObserved("SMS") {
		t.Fatal("RecognizesObserved(SMS) = false, want registered shared core name")
	}
	if registry.RecognizesObserved("UNKNOWN") {
		t.Fatal("RecognizesObserved(UNKNOWN) = true")
	}
	for _, system := range []protocol.System{protocol.SystemSMS, protocol.SystemGameGear} {
		spec, ok := registry.LookupObservedForSystem("SMS", system)
		if !ok || spec.System != system || spec.ExpectedCore != "SMS" {
			t.Fatalf("LookupObservedForSystem(SMS, %s) = %#v, %v", system, spec, ok)
		}
	}
	gameBoy, ok := registry.LookupObserved("GAMEBOY")
	if !ok || gameBoy.System != protocol.SystemGameBoy {
		t.Fatalf("LookupObserved(GAMEBOY) = %#v, %v; canonical GB fallback must remain available without launch intent", gameBoy, ok)
	}
	if spec, ok := registry.LookupObservedForSystem("ATARI7800", protocol.SystemAtari2600); !ok || spec.System != protocol.SystemAtari2600 {
		t.Fatalf("ATARI7800 intent lookup = %#v, %v", spec, ok)
	}
	if spec, ok := registry.LookupObserved("ATARI7800"); !ok || spec.System != protocol.SystemAtari7800 {
		t.Fatalf("ATARI7800 default lookup = %#v, %v", spec, ok)
	}
	if spec, ok := registry.LookupObserved("WonderSwan"); !ok || spec.System != protocol.SystemWonderSwan {
		t.Fatalf("WonderSwan default lookup = %#v, %v", spec, ok)
	}
	for _, system := range []protocol.System{protocol.SystemWonderSwan, protocol.SystemWonderSwanColor} {
		spec, ok := registry.LookupObservedForSystem("WonderSwan", system)
		if !ok || spec.System != system || spec.ExpectedCore != "WonderSwan" {
			t.Fatalf("LookupObservedForSystem(WonderSwan, %s) = %#v, %v", system, spec, ok)
		}
	}
	for _, system := range []protocol.System{protocol.SystemGameBoy, protocol.SystemGameBoyColor} {
		spec, ok := registry.LookupObservedForSystem("GAMEBOY", system)
		if !ok || spec.System != system || spec.ExpectedCore != "GAMEBOY" {
			t.Fatalf("LookupObservedForSystem(GAMEBOY, %s) = %#v, %v", system, spec, ok)
		}
	}
	if _, ok := registry.LookupObservedForSystem("SMS", protocol.SystemSNES); ok {
		t.Fatal("LookupObservedForSystem(SMS, SNES) = true")
	}
}

func TestLookupReturnsDefensiveExtensionCopy(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	first, ok := registry.Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry missing")
	}
	delete(first.Extensions, ".sfc")
	second, ok := registry.Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry missing after mutation")
	}
	if _, ok := second.Extensions[".sfc"]; !ok {
		t.Fatal("caller mutation changed the registry")
	}
}

func TestLookupObservedDoesNotFirstWinArbitraryDuplicate(t *testing.T) {
	t.Parallel()
	registry := core.NewRegistry(
		core.Spec{System: "one", ExpectedCore: "SHARED"},
		core.Spec{System: "two", ExpectedCore: "SHARED"},
		core.Spec{System: "three", ExpectedCore: "SHARED"},
	)
	if _, ok := registry.LookupObserved("SHARED"); ok {
		t.Fatal("LookupObserved(SHARED) = true for arbitrary ambiguous candidates")
	}
	if !registry.RecognizesObserved("SHARED") {
		t.Fatal("RecognizesObserved(SHARED) = false")
	}
}

func TestLookupObservedUsesDeclaredFallbackRegardlessOfSpecOrder(t *testing.T) {
	t.Parallel()
	preferred := core.Spec{System: protocol.SystemGameBoy, ExpectedCore: "GAMEBOY", ObservedFallback: true}
	alternate := core.Spec{System: protocol.SystemGameBoyColor, ExpectedCore: "GAMEBOY"}
	for _, specs := range [][]core.Spec{{preferred, alternate}, {alternate, preferred}} {
		registry := core.NewRegistry(specs...)
		spec, ok := registry.LookupObserved("GAMEBOY")
		if !ok || spec.System != protocol.SystemGameBoy {
			t.Fatalf("LookupObserved(GAMEBOY) = %#v, %v", spec, ok)
		}
	}
}

func TestSpecsReturnsAllEntriesInStableOrderAndDefensiveCopies(t *testing.T) {
	t.Parallel()

	registry := core.NewRegistry(
		core.Spec{System: protocol.SystemSNES, Extensions: map[string]struct{}{".sfc": {}}},
		core.Spec{System: protocol.SystemNES, Extensions: map[string]struct{}{".nes": {}}},
		core.Spec{System: protocol.SystemSMS, Extensions: map[string]struct{}{".sms": {}}},
		core.Spec{System: protocol.SystemMegaDrive, Extensions: map[string]struct{}{".md": {}}},
	)

	specs := registry.Specs()
	wantSystems := []protocol.System{protocol.SystemMegaDrive, protocol.SystemNES, protocol.SystemSMS, protocol.SystemSNES}
	if len(specs) != len(wantSystems) {
		t.Fatalf("Specs() returned %d entries, want %d", len(specs), len(wantSystems))
	}
	for index, wantSystem := range wantSystems {
		if specs[index].System != wantSystem {
			t.Fatalf("Specs()[%d].System = %q, want %q", index, specs[index].System, wantSystem)
		}
	}
	delete(specs[0].Extensions, ".md")
	again := registry.Specs()
	if _, ok := again[0].Extensions[".md"]; !ok {
		t.Fatal("caller mutation changed the registry")
	}
}
