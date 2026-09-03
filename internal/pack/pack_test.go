package pack

import (
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestLoadAndOracle(t *testing.T) {
	root := repoRoot(t)
	resolved, err := LoadPlatform(filepath.Join(root, "packages", "platform", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Board.FPGADevice != "5CSEBA6U23I7" {
		t.Fatalf("part %q", resolved.Board.FPGADevice)
	}
	got, err := resolved.SymbolMap()
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := LoadOracle(filepath.Join(root, "testdata", "oracles", "libmister-runtime-fpga.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	problems := DiffOracle(got, oracle)
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestDuplicateSymbolConflict(t *testing.T) {
	r := &Resolved{
		SoC: SoCFile{
			Windows: []Window{
				{Name: "A", Base: 1, BaseSymbol: "X"},
				{Name: "B", Base: 2, BaseSymbol: "X"},
			},
		},
	}
	if _, err := r.Symbols(); err == nil {
		t.Fatal("expected duplicate symbol error")
	}
}

func TestLoadSystemAndOracle(t *testing.T) {
	root := repoRoot(t)
	sys, err := LoadSystem(filepath.Join(root, "packages", "system", "megadrive.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sys.ExpectedCore != "MegaDrive" {
		t.Fatalf("core %q", sys.ExpectedCore)
	}
	oracle, err := LoadSystemOracle(filepath.Join(root, "testdata", "oracles", "libmister-runtime-megadrive.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	problems := DiffSystemOracle(sys, oracle)
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestSystemDuplicateMediaRole(t *testing.T) {
	sys := validMegaDrive()
	sys.Media = append(sys.Media, sys.Media[0])
	if err := sys.Validate(); err == nil {
		t.Fatal("expected duplicate media role error")
	}
}

func validMegaDrive() SystemFile {
	return SystemFile{
		ID:           "megadrive",
		ExpectedCore: "MegaDrive",
		RBF:          RBFRef{Role: "core", Artifact: "megadrive.rbf"},
		Media: []MediaRule{{
			Role: "cartridge", Index: 1, Required: true,
			Extensions: []string{".md", ".gen", ".bin"}, MaximumSize: 0x2000000,
		}},
		Core: CoreRecipe{
			ResetAssertWord: 1, InitialStatusWord: 1, ResetReleaseWord: 0,
			FileWire: FileWireLittleEndianBytePairs,
		},
		Input: InputRecipe{
			PlayerCount: 1, PlayerCommand: 0x02,
			Up: 0x8, Down: 0x4, Left: 0x2, Right: 0x1,
			A: 0x10, B: 0x20, C: 0x40, Start: 0x80,
		},
	}
}
