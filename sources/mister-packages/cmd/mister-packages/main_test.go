package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsAcceptABIAndProgrammingDefinitions(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	abiPath := filepath.Join(root, "packages", "abi", "fes_simple_game.yaml")
	computerPath := filepath.Join(root, "packages", "abi", "fes_simple_computer.yaml")
	profilesPath := filepath.Join(root, "packages", "programming", "de10_nano.yaml")
	for path, want := range map[string]string{
		abiPath:      "fes.simple-game",
		computerPath: "fes.simple-computer",
		profilesPath: "de10_nano",
	} {
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
	computerVerilog, err := emitVerilogPath(computerPath)
	if err != nil || !strings.Contains(computerVerilog, "`define FES_SIMPLE_COMPUTER_SIGNATURE") {
		t.Fatalf("emit-verilog computer ABI = %q, %v", computerVerilog, err)
	}
}

func TestCommandsKeepFabricSeparateFromHostABI(t *testing.T) {
	path := filepath.Join("..", "..", "packages", "fabric", "fes_fabric_video_raster_rgb888.yaml")
	id, err := validatePath(path)
	if err != nil || id != "fes.fabric.video.raster-rgb888" {
		t.Fatalf("validate fabric = %q, %v", id, err)
	}
	verilog, err := emitVerilogPath(path)
	if err != nil || !strings.Contains(verilog, "`define FES_VIDEO_PART_REQUEST_BITS") {
		t.Fatalf("emit-verilog fabric = %q, %v", verilog, err)
	}
	if _, err := emitGoPath(path); err == nil {
		t.Fatal("fabric unexpectedly accepted as a host Go ABI")
	}
	if _, err := emitPath(path); err == nil {
		t.Fatal("fabric unexpectedly accepted as a host C++ ABI")
	}
}
