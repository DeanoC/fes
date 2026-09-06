package pack

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSNESContract(t *testing.T) {
	sys, err := LoadSystem(filepath.Join(repoRoot(t), "packages/system/snes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sys.ExpectedCore != "SNES" || sys.RBF.Artifact != "snes.rbf" || sys.CoreSource == nil || sys.CoreSource.Commit != "93d359e6f23c734ae3928984e88bed1d9b53cbac" {
		t.Fatalf("wrong pinned SNES: %+v", sys)
	}
	if len(sys.Media) != 1 || sys.Media[0].Index != 1 || sys.Media[0].Transform != "snes_cartridge" || !sys.Media[0].Required || uint64(sys.Media[0].MaximumSize) != 0x400200 {
		t.Fatalf("wrong media: %+v", sys.Media)
	}
	if sys.Input.C != 0 || sys.Input.X != 0x40 || sys.Input.Y != 0x80 || sys.Input.L != 0x100 || sys.Input.R != 0x200 || sys.Input.Select != 0x400 || sys.Input.Start != 0x800 {
		t.Fatalf("wrong input: %+v", sys.Input)
	}
}

func TestOptionalInputAndTransformValidation(t *testing.T) {
	sys, err := LoadSystem(filepath.Join(repoRoot(t), "packages/system/megadrive.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sys.Input.C = 0
	if err := sys.Validate(); err != nil {
		t.Fatalf("optional C: %v", err)
	}
	sys.Input.X = sys.Input.A
	if err := sys.Validate(); err == nil {
		t.Fatal("accepted overlapping optional mask")
	}
	sys.Input.X = 0
	sys.Input.Start = 0
	if err := sys.Validate(); err == nil {
		t.Fatal("accepted missing common Start")
	}
	sys.Input.Start = 0x80
	sys.Media[0].Transform = "unknown"
	if err := sys.Validate(); err == nil || !strings.Contains(err.Error(), "transform") {
		t.Fatalf("invalid transform: %v", err)
	}
	sys.Media[0].Transform = "raw"
	if err := sys.Validate(); err != nil {
		t.Fatal(err)
	}
}
