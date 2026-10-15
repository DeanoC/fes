package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
)

func TestPackInspectDeterministic(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "bin"), 0755); e != nil {
		t.Fatal(e)
	}
	for _, p := range software.RequiredExecutables {
		if e := os.WriteFile(filepath.Join(root, p), []byte("test executable "+p), 0755); e != nil {
			t.Fatal(e)
		}
	}
	m := software.Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: "fes-base-v1", Version: "1", Interfaces: map[string]string{"runtime": "2"}, Provenance: map[string]string{}}
	meta := filepath.Join(t.TempDir(), "metadata.json")
	b, _ := json.Marshal(m)
	if e := os.WriteFile(meta, b, 0600); e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(t.TempDir(), "release.tar")
	var stdout bytes.Buffer
	args := []string{"pack", "--manifest", meta, "--root", root, "--output", output}
	if e := run(args, &stdout); e != nil {
		t.Fatal(e)
	}
	first, _ := os.ReadFile(output)
	firstJSON := append([]byte(nil), stdout.Bytes()...)
	stdout.Reset()
	if e := run(args, &stdout); e != nil {
		t.Fatal(e)
	}
	second, _ := os.ReadFile(output)
	if !bytes.Equal(first, second) {
		t.Fatal("non-deterministic bundle")
	}
	stdout.Reset()
	if e := run([]string{"inspect", "--bundle", output}, &stdout); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(stdout.Bytes(), firstJSON) {
		t.Fatal("inspection identity differs")
	}
	if e := os.Symlink("/etc/passwd", filepath.Join(root, "bin", "bad")); e != nil {
		t.Fatal(e)
	}
	if e := run(args, &stdout); e == nil {
		t.Fatal("accepted symlink")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(first, after) {
		t.Fatal("failed build replaced output")
	}
}
