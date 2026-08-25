package systems

import (
	"path"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestTableInvariants(t *testing.T) {
	platforms := make(map[protocol.System]struct{})
	launchSystems := make(map[protocol.System]struct{})
	aliases := make(map[string]struct{})
	fpga := 0
	for _, row := range Rows() {
		if row.PlatformID == "" || row.Label == "" || len(row.Extensions) == 0 {
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
			if row.Core == nil || row.Core.ExpectedCore == "" || row.Core.RBF == "" || row.Core.KitROMRoot == "" || row.Core.MGLRoot == "" || row.LaunchSystem == "" || row.LaunchSystem != row.PlatformID {
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
		for provider, cover := range row.CoverSlugs {
			if strings.TrimSpace(provider) == "" || strings.TrimSpace(cover.Slug) == "" || strings.TrimSpace(cover.Name) == "" {
				t.Fatalf("invalid cover mapping for %q: %q %#v", row.PlatformID, provider, cover)
			}
		}
	}
	if fpga != 4 {
		t.Fatalf("FPGA rows = %d, want 4", fpga)
	}
	if !Mapped(protocol.SystemSNES) || !Mapped(protocol.SystemMegaDrive) || !Mapped(protocol.SystemNES) || !Mapped(protocol.SystemSMS) {
		t.Fatal("folder mappings do not match the approved slice")
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
		{protocol.SystemNES, "NES", []string{".nes", ".unf", ".unif", ".fds"}, "NES", "_Console/NES", "/media/fat/games/NES", "/media/fat/games/NES", 1, 0, "nes", "Nintendo Entertainment System"},
		{protocol.SystemSMS, "SMS", []string{".sms"}, "SMS", "_Console/SMS", "/media/fat/games/SMS", "/media/fat/games/SMS", 1, 1, "sms", "Sega Master System/Mark III"},
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
}
