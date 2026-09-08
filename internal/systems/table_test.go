package systems

import (
	"path"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/systems/generated"
	"github.com/DeanoC/FogCast/protocol"
)

func TestTableInvariants(t *testing.T) {
	platforms := make(map[protocol.System]struct{})
	launchSystems := make(map[protocol.System]struct{})
	aliases := make(map[string]struct{})
	fpga := 0
	for _, row := range Rows() {
		if row.PlatformID == "" || row.Label == "" || (len(row.Extensions) == 0 && (row.Core == nil || !row.Core.ROMless)) {
			t.Fatalf("incomplete row: %#v", row)
		}
		if _, exists := platforms[row.PlatformID]; exists {
			t.Fatalf("duplicate platform %q", row.PlatformID)
		}
		platforms[row.PlatformID] = struct{}{}
		if row.FolderAlias != "" {
			alias := strings.ToLower(row.FolderAlias)
			if path.Clean(row.FolderAlias) != row.FolderAlias || strings.Contains(row.FolderAlias, "/") {
				t.Fatalf("invalid folder alias %q", row.FolderAlias)
			}
			if _, exists := aliases[alias]; exists {
				t.Fatalf("duplicate folder alias %q", row.FolderAlias)
			}
			aliases[alias] = struct{}{}
		}
		extensions := make(map[string]struct{}, len(row.Extensions))
		for _, extension := range row.Extensions {
			if extension == "" || extension != strings.ToLower(extension) || !strings.HasPrefix(extension, ".") {
				t.Fatalf("invalid extension %q for %q", extension, row.PlatformID)
			}
			if _, exists := extensions[extension]; exists {
				t.Fatalf("duplicate extension %q for %q", extension, row.PlatformID)
			}
			extensions[extension] = struct{}{}
		}
		switch row.Capability {
		case CapabilityCatalog:
			if row.Core != nil || row.LaunchSystem != "" {
				t.Fatalf("catalog row %q has launch data", row.PlatformID)
			}
		case CapabilityFPGANative:
			fpga++
			if row.Core == nil || row.Core.ExpectedCore == "" || (!row.Core.ROMless && (row.Core.RBF == "" || row.Core.KitROMRoot == "" || row.Core.MGLRoot == "")) || row.LaunchSystem == "" || row.LaunchSystem != row.PlatformID {
				t.Fatalf("FPGA row %q lacks core data", row.PlatformID)
			}
			if err := protocol.ValidateSystem(row.PlatformID); err != nil {
				t.Fatalf("FPGA row %q is not a protocol system: %v", row.PlatformID, err)
			}
			if _, exists := launchSystems[row.LaunchSystem]; exists {
				t.Fatalf("duplicate FPGA launch system %q", row.LaunchSystem)
			}
			launchSystems[row.LaunchSystem] = struct{}{}
		case CapabilityHostOnly:
		default:
			t.Fatalf("invalid capability %q", row.Capability)
		}
		if row.Core != nil {
			for _, prerequisite := range row.Core.RequiredFiles {
				if path.IsAbs(prerequisite) || path.Clean(prerequisite) != prerequisite || prerequisite == "." || prerequisite == ".." || strings.HasPrefix(prerequisite, "../") {
					t.Fatalf("invalid target prerequisite %q for %q", prerequisite, row.PlatformID)
				}
			}
		}
		for provider, cover := range row.CoverSlugs {
			if strings.TrimSpace(provider) == "" || strings.TrimSpace(cover.Slug) == "" || strings.TrimSpace(cover.Name) == "" {
				t.Fatalf("invalid cover mapping for %q: %q %#v", row.PlatformID, provider, cover)
			}
		}
	}
	if fpga != 17 {
		t.Fatalf("FPGA rows = %d, want 17", fpga)
	}
}

func TestMegaDriveLaunchFieldsComeFromPackage(t *testing.T) {
	var mega *Row
	for i := range Rows() {
		if Rows()[i].PlatformID == protocol.SystemMegaDrive {
			row := Rows()[i]
			mega = &row
			break
		}
	}
	if mega == nil || mega.Core == nil {
		t.Fatal("missing Mega Drive FPGA row")
	}
	if mega.Core.ExpectedCore != generated.MegaDriveExpectedCore || generated.MegaDriveExpectedCore != "MegaDrive" {
		t.Fatalf("expected core %q", mega.Core.ExpectedCore)
	}
	if mega.Core.FileIndex != generated.MegaDriveCartridgeIndex || generated.MegaDriveCartridgeIndex != 1 {
		t.Fatalf("file index %d", mega.Core.FileIndex)
	}
	if mega.Core.RBF != "_Console/MegaDrive" || mega.Core.KitROMRoot == "" {
		t.Fatalf("product fields were replaced: %#v", mega.Core)
	}
	for _, system := range []protocol.System{
		protocol.SystemMegaDrive, protocol.SystemSNES, protocol.SystemNES, protocol.SystemSMS,
		protocol.SystemGameBoy, protocol.SystemGameBoyColor, protocol.SystemGBA, protocol.SystemPCE,
		protocol.SystemGameGear, protocol.SystemAtari2600, protocol.SystemAtari7800, protocol.SystemColecoVision,
		protocol.SystemAtariLynx, protocol.SystemWonderSwan, protocol.SystemWonderSwanColor, protocol.SystemIntellivision,
	} {
		if !Mapped(system) {
			t.Fatalf("folder mapping missing for %q", system)
		}
	}
}

func TestNESLaunchFieldsComeFromPackage(t *testing.T) {
	row, ok := Lookup(protocol.SystemNES)
	if !ok || row.Core == nil {
		t.Fatal("missing NES FPGA row")
	}
	if row.Core.ExpectedCore != generated.NESExpectedCore || generated.NESExpectedCore != "NES" {
		t.Fatalf("expected core %q", row.Core.ExpectedCore)
	}
	if row.Core.FileIndex != generated.NESCartridgeIndex || generated.NESCartridgeIndex != 0 {
		t.Fatalf("file index %d", row.Core.FileIndex)
	}
}

func TestRowsAndLookupReturnDefensiveCopies(t *testing.T) {
	rows := Rows()
	rows[0].Extensions[0] = ".changed"
	rows[0].Core.ExpectedCore = "changed"
	rows[0].CoverSlugs[CoverProviderIGDB] = CoverSpec{Slug: "changed"}

	row, ok := Lookup(protocol.SystemMegaDrive)
	if !ok || row.Extensions[0] != ".md" || row.Core.ExpectedCore != "MegaDrive" || row.CoverSlugs[CoverProviderIGDB].Slug != "genesis-slash-megadrive" {
		t.Fatalf("table mutated through copy: %#v", row)
	}
}

func TestSMBFolderUsesMappedAlias(t *testing.T) {
	snes, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemSNES)
	if !ok || snes != "//deano-clawz/Games/Games/SNES" {
		t.Fatalf("SNES folder = %q, %v", snes, ok)
	}
	mega, ok := SMBFolder(DefaultSMBShareRoot+"/", protocol.SystemMegaDrive)
	if !ok || mega != "//deano-clawz/Games/Games/Genesis" {
		t.Fatalf("Mega Drive folder = %q, %v", mega, ok)
	}
	nes, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemNES)
	if !ok || nes != "//deano-clawz/Games/Games/NES" {
		t.Fatalf("NES folder = %q, %v", nes, ok)
	}
	sms, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemSMS)
	if !ok || sms != "//deano-clawz/Games/Games/SMS" {
		t.Fatalf("SMS folder = %q, %v", sms, ok)
	}
	for _, test := range []struct {
		system protocol.System
		alias  string
	}{
		{protocol.SystemGameBoy, "gb"},
		{protocol.SystemGBA, "gba"},
		{protocol.SystemPCE, "pce"},
		{protocol.SystemGameGear, "gg"},
		{protocol.SystemGameBoyColor, "Game Boy Color"},
		{protocol.SystemAtari2600, "Atari2600"},
		{protocol.SystemColecoVision, "ColecoVision"},
		{protocol.SystemAtariLynx, "AtariLynx"},
		{protocol.SystemWonderSwan, "WonderSwan"},
		{protocol.SystemWonderSwanColor, "WonderSwan Color"},
		{protocol.SystemAtari7800, "Atari7800"},
		{protocol.SystemIntellivision, "Intellivision"},
	} {
		folder, ok := SMBFolder(DefaultSMBShareRoot, test.system)
		want := DefaultSMBShareRoot + "/" + test.alias
		if !ok || folder != want {
			t.Fatalf("%s folder = %q, %v; want %q", test.system, folder, ok, want)
		}
	}
}

func TestFPGAExtensionAndCoverRows(t *testing.T) {
	for _, test := range []struct {
		system                              protocol.System
		alias                               string
		extensions                          []string
		expectedCore, rbf, romRoot, mglRoot string
		delay, index                        int
		slug, name                          string
	}{
		{protocol.SystemNES, "NES", []string{".nes"}, "NES", "_Console/NES", "/media/fat/games/NES", "/media/fat/games/NES", 1, 0, "nes", "Nintendo Entertainment System"},
		{protocol.SystemSMS, "SMS", []string{".sms"}, "SMS", "_Console/SMS", "/media/fat/games/SMS", "/media/fat/games/SMS", 1, 1, "sms", "Sega Master System/Mark III"},
		{protocol.SystemGameBoy, "gb", []string{".gb"}, "GAMEBOY", "_Console/Gameboy", "/media/fat/games/Gameboy", "/media/fat/games/Gameboy", 2, 1, "gb", "Game Boy"},
		{protocol.SystemGBA, "gba", []string{".gba"}, "GBA", "_Console/GBA", "/media/fat/games/GBA", "/media/fat/games/GBA", 2, 0, "gba", "Game Boy Advance"},
		{protocol.SystemPCE, "pce", []string{".pce"}, "TGFX16", "_Console/TurboGrafx16", "/media/fat/games/TGFX16", "/media/fat/games/TGFX16", 1, 0, "turbografx16--1", "TurboGrafx-16/PC Engine"},
		{protocol.SystemGameGear, "gg", []string{".gg"}, "SMS", "_Console/SMS", "/media/fat/games/SMS", "/media/fat/games/SMS", 1, 2, "game-gear", "Sega Game Gear"},
		{protocol.SystemGameBoyColor, "Game Boy Color", []string{".gbc"}, "GAMEBOY", "_Console/Gameboy", "/media/fat/games/Gameboy", "/media/fat/games/Gameboy", 2, 1, "gbc", "Game Boy Color"},
		{protocol.SystemAtari2600, "Atari2600", []string{".a26", ".bin"}, "ATARI7800", "_Console/Atari7800", "/media/fat/games/Atari2600", "/media/fat/games/ATARI7800", 1, 1, "atari2600", "Atari 2600"},
		{protocol.SystemColecoVision, "ColecoVision", []string{".col", ".bin", ".rom"}, "Coleco", "_Console/ColecoVision", "/media/fat/games/Coleco", "/media/fat/games/Coleco", 1, 0, "colecovision", "ColecoVision"},
		{protocol.SystemAtariLynx, "AtariLynx", []string{".lnx", ".lyx"}, "AtariLynx", "_Console/AtariLynx", "/media/fat/games/AtariLynx", "/media/fat/games/AtariLynx", 1, 0, "lynx", "Atari Lynx"},
		{protocol.SystemWonderSwan, "WonderSwan", []string{".ws"}, "WonderSwan", "_Console/WonderSwan", "/media/fat/games/WonderSwan", "/media/fat/games/WonderSwan", 1, 1, "wonderswan", "WonderSwan"},
		{protocol.SystemWonderSwanColor, "WonderSwan Color", []string{".wsc"}, "WonderSwan", "_Console/WonderSwan", "/media/fat/games/WonderSwanColor", "/media/fat/games/WonderSwan", 1, 1, "wonderswan-color", "WonderSwan Color"},
		{protocol.SystemAtari7800, "Atari7800", []string{".a78", ".bin"}, "ATARI7800", "_Console/Atari7800", "/media/fat/games/ATARI7800", "/media/fat/games/ATARI7800", 1, 1, "atari7800", "Atari 7800"},
		{protocol.SystemIntellivision, "Intellivision", []string{".rom", ".int", ".bin"}, "Intellivision", "_Console/Intellivision", "/media/fat/games/Intellivision", "/media/fat/games/Intellivision", 1, 0, "intellivision", "Intellivision"},
	} {
		row, ok := Lookup(test.system)
		if !ok || row.Capability != CapabilityFPGANative || row.FolderAlias != test.alias || row.LaunchSystem != test.system {
			t.Fatalf("row %q = %#v, ok=%v", test.system, row, ok)
		}
		if strings.Join(row.Extensions, ",") != strings.Join(test.extensions, ",") {
			t.Fatalf("extensions for %q = %v, want %v", test.system, row.Extensions, test.extensions)
		}
		if row.Core == nil || row.Core.ExpectedCore != test.expectedCore || row.Core.RBF != test.rbf || row.Core.KitROMRoot != test.romRoot || row.Core.MGLRoot != test.mglRoot || row.Core.FileDelay != test.delay || row.Core.FileType != "f" || row.Core.FileIndex != test.index {
			t.Fatalf("core for %q = %#v", test.system, row.Core)
		}
		cover := row.CoverSlugs[CoverProviderIGDB]
		if cover.Slug != test.slug || cover.Name != test.name {
			t.Fatalf("cover for %q = %#v", test.system, cover)
		}
	}
	lynx, ok := Lookup(protocol.SystemAtariLynx)
	if !ok || lynx.Core == nil || len(lynx.Core.RequiredFiles) != 1 || lynx.Core.RequiredFiles[0] != "boot.rom" {
		t.Fatalf("Lynx prerequisites = %#v, %v", lynx.Core, ok)
	}
	for _, test := range []struct {
		system protocol.System
		files  []string
	}{
		{protocol.SystemWonderSwan, []string{"boot.rom", "boot1.rom"}},
		{protocol.SystemWonderSwanColor, []string{"boot.rom", "boot1.rom"}},
		{protocol.SystemIntellivision, []string{"boot.rom"}},
	} {
		row, ok := Lookup(test.system)
		if !ok || row.Core == nil || strings.Join(row.Core.RequiredFiles, ",") != strings.Join(test.files, ",") {
			t.Fatalf("%s prerequisites = %#v, %v; want %v", test.system, row.Core, ok, test.files)
		}
	}
}
