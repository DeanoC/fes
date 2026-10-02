package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionDisplayMatchesSharedLayoutAndRuntimeOracle(t *testing.T) {
	root := repoRoot(t)
	computer, err := LoadABI(filepath.Join(root, "packages/abi/fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := LoadABI(filepath.Join(root, "packages/abi/fes_application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Source     struct{ Repository, Commit, File, Note string }
		Constants  map[string]uint32
		Interfaces []struct {
			ID            string
			Major, Minor  uint16
			CapabilityBit uint8 `json:"capability_bit"`
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata/oracles/fes-simple-computer-session-display.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Source.Repository != "DeanoC/fes" || len(oracle.Source.Commit) != 40 ||
		oracle.Source.File != "sources/libmister-runtime/src/native/generated/fes_simple_computer.hpp" ||
		!strings.Contains(oracle.Source.Note, "issue 370") {
		t.Fatal("runtime contract provenance is missing")
	}
	if len(oracle.Constants) < 40 || len(oracle.Interfaces) != 2 || len(computer.Interfaces) != 7 {
		t.Fatal("session display oracle is incomplete")
	}
	for name, want := range oracle.Constants {
		got, ok := computer.Constant(name)
		shared, sharedOK := app.Constant("FesApplication" + strings.TrimPrefix(name, "FesSimpleComputer"))
		if !ok || !sharedOK || got != want || shared != want {
			t.Errorf("%s: computer=%x shared=%x oracle=%x", name, got, shared, want)
		}
	}
	for i, want := range oracle.Interfaces {
		got := computer.Interfaces[5+i]
		if got.ID != want.ID || got.Major != want.Major || got.Minor != want.Minor || got.CapabilityBit != want.CapabilityBit {
			t.Fatalf("session interface %d differs: %#v, %#v", i, got, want)
		}
	}
}
