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
