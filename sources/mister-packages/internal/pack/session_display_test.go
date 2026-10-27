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

func TestApplicationSessionDisplayMatchesRuntimeOracle(t *testing.T) {
	root := repoRoot(t)
	app, err := LoadABI(filepath.Join(root, "packages/abi/fes_application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Source struct {
			Repository string
			Commit     string
			File       string
			Note       string
		}
		Interface struct {
			ID             string `json:"id"`
			Major          uint16 `json:"major"`
			Minor          uint16 `json:"minor"`
			CapabilityBit  uint8  `json:"capability_bit"`
			CapabilityMask uint32 `json:"capability_mask"`
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata/oracles/fes-application-session-display.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Source.Repository != "DeanoC/fes" ||
		oracle.Source.Commit != "736e77c061f189f03a6e9911a2636ea10671630c" ||
		oracle.Source.File != "sources/libmister-runtime/src/native/generated/fes_application.hpp" ||
		!strings.Contains(oracle.Source.Note, "in-tree runtime") {
		t.Fatalf("runtime contract provenance is missing: %+v", oracle.Source)
	}
	var matches []ABIInterface
	for _, got := range app.Interfaces {
		if got.ID == oracle.Interface.ID {
			matches = append(matches, got)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("application session-display interface count %d", len(matches))
	}
	got := matches[0]
	if got.Major != oracle.Interface.Major || got.Minor != oracle.Interface.Minor ||
		got.CapabilityBit != oracle.Interface.CapabilityBit ||
		got.ID != "fes.video.session-display" || got.CapabilityBit != 11 ||
		oracle.Interface.CapabilityMask != 1<<11 ||
		uint32(1)<<got.CapabilityBit != oracle.Interface.CapabilityMask {
		t.Fatalf("application session-display contract: got %+v oracle %+v", got, oracle.Interface)
	}
}
