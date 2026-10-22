package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayPlaneMatchesRuntimeOracle(t *testing.T) {
	root := repoRoot(t)
	fabric, err := LoadFabric(filepath.Join(root, "packages/fabric/fes_fabric_video_display_plane.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Source struct{ Repository, Commit, File, Note string }
		Fabric struct {
			ID           string
			Major, Minor uint16
		}
		Constants map[string]uint32
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata/oracles/fes-display-plane.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Source.Repository != "DeanoC/fes" || len(oracle.Source.Commit) != 40 ||
		oracle.Source.File != "sources/libmister-runtime/src/native/generated/fes_display_plane.hpp" ||
		!strings.Contains(oracle.Source.Note, "issue 947") {
		t.Fatalf("runtime consumer revision: %+v", oracle.Source)
	}
	if fabric.ID != oracle.Fabric.ID || fabric.Major != oracle.Fabric.Major || fabric.Minor != oracle.Fabric.Minor {
		t.Fatalf("fabric %s %d.%d; oracle %+v", fabric.ID, fabric.Major, fabric.Minor, oracle.Fabric)
	}
	// The oracle covers every plane constant, so adding or changing one needs
	// an oracle change.
	if len(oracle.Constants) != len(fabric.Constants) {
		t.Fatalf("oracle has %d constants; fabric has %d", len(oracle.Constants), len(fabric.Constants))
	}
	for name, want := range oracle.Constants {
		if got, ok := fabric.Constant(name); !ok || got != want {
			t.Errorf("%s=%x present=%t want=%x", name, got, ok, want)
		}
	}
}
