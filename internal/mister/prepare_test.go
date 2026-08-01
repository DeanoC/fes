package mister_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestPrepareLaunch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rom := filepath.Join(root, "RPGs", "test & demo.sfc")
	if err := os.MkdirAll(filepath.Dir(rom), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rom, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	spec.ROMRoot = root
	prepared, apiErr := mister.PrepareLaunch(spec, rom)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if prepared.RelativeROM != "RPGs/test & demo.sfc" {
		t.Fatalf("relative path = %q", prepared.RelativeROM)
	}
	if len(prepared.MGL) == 0 {
		t.Fatal("empty MGL")
	}
}

func TestPrepareLaunchRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sfc")
	if err := os.WriteFile(outside, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	insideWrongExtension := filepath.Join(root, "wrong.zip")
	if err := os.WriteFile(insideWrongExtension, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.sfc")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	spec.ROMRoot = root
	tests := []struct {
		name string
		path string
		code protocol.ErrorCode
	}{
		{name: "relative", path: "test.sfc", code: protocol.CodeInvalidROMPath},
		{name: "nul", path: root + "/bad\x00.sfc", code: protocol.CodeInvalidROMPath},
		{name: "outside", path: outside, code: protocol.CodeInvalidROMPath},
		{name: "symlink escape", path: link, code: protocol.CodeInvalidROMPath},
		{name: "extension", path: insideWrongExtension, code: protocol.CodeInvalidROMPath},
		{name: "missing", path: filepath.Join(root, "missing.sfc"), code: protocol.CodeROMNotFound},
		{name: "root", path: root, code: protocol.CodeInvalidROMPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, apiErr := mister.PrepareLaunch(spec, tt.path)
			if apiErr == nil || apiErr.Code != tt.code {
				t.Fatalf("error = %#v, want code %s", apiErr, tt.code)
			}
		})
	}
}
