package pack

import (
	"path/filepath"
	"testing"
)

func TestNESContract(t *testing.T) {
	sys, err := LoadSystem(filepath.Join(repoRoot(t), "packages/system/nes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sys.ExpectedCore != "NES" || sys.RBF.Artifact != "nes.rbf" ||
		sys.CoreSource == nil || sys.CoreSource.Commit != "9a63821173b6da4d6e95dcbe2e2a322ec8171144" {
		t.Fatalf("wrong NES pin: %+v", sys)
	}
	if len(sys.Media) != 1 || sys.Media[0].Index != 0x40 ||
		sys.Media[0].Transform != "nes_cartridge" ||
		len(sys.Media[0].Extensions) != 1 || sys.Media[0].Extensions[0] != ".nes" ||
		uint64(sys.Media[0].MaximumSize) != 0x2000000 {
		t.Fatalf("wrong NES media: %+v", sys.Media)
	}
	if sys.Input.A != 0x10 || sys.Input.B != 0x20 ||
		sys.Input.Select != 0x400 || sys.Input.Start != 0x800 {
		t.Fatalf("wrong NES input: %+v", sys.Input)
	}
	if sys.Core.FileWire != FileWireLittleEndianBytes {
		t.Fatalf("wrong NES file wire: %q", sys.Core.FileWire)
	}
}
