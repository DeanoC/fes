package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsAcceptABIAndProgrammingDefinitions(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	abiPath := filepath.Join(root, "packages", "abi", "fes_simple_game.yaml")
	profilesPath := filepath.Join(root, "packages", "programming", "de10_nano.yaml")
	for path, want := range map[string]string{abiPath: "fes.simple-game", profilesPath: "de10_nano"} {
		got, err := validatePath(path)
		if err != nil || got != want {
			t.Fatalf("validatePath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	cpp, err := emitPath(abiPath)
	if err != nil || !strings.Contains(cpp, "FesGpSignature") {
		t.Fatalf("emit-cpp ABI = %q, %v", cpp, err)
	}
	goOutput, err := emitGoPath(profilesPath)
	if err != nil || !strings.Contains(goOutput, "De10NanoProgrammingProfilePairs") {
		t.Fatalf("emit-go registry = %q, %v", goOutput, err)
	}
	verilog, err := emitVerilogPath(abiPath)
	if err != nil || !strings.Contains(verilog, "`define FES_GP_SIGNATURE") {
		t.Fatalf("emit-verilog ABI = %q, %v", verilog, err)
	}
}
