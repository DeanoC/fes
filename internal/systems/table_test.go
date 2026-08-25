package systems

import (
	"path"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestTableInvariants(t *testing.T) {
	platforms := make(map[protocol.System]struct{})
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
			if row.Core != nil {
				t.Fatalf("catalog row %q has core data", row.PlatformID)
			}
		case CapabilityFPGANative:
			fpga++
			if row.Core == nil || row.Core.ExpectedCore == "" || row.Core.RBF == "" || row.Core.KitROMRoot == "" || row.Core.MGLRoot == "" {
				t.Fatalf("FPGA row %q lacks core data", row.PlatformID)
			}
			if err := protocol.ValidateSystem(row.PlatformID); err != nil {
				t.Fatalf("FPGA row %q is not a protocol system: %v", row.PlatformID, err)
			}
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
	if fpga != 2 {
		t.Fatalf("FPGA rows = %d, want 2", fpga)
	}
	if !Mapped(protocol.SystemSNES) || !Mapped(protocol.SystemMegaDrive) || Mapped("nes") {
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
	if _, ok := SMBFolder(DefaultSMBShareRoot, "nes"); ok {
		t.Fatal("catalog-only NES unexpectedly has an SMB mapping")
	}
}
