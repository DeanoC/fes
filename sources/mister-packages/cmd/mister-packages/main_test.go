package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsAcceptStandaloneBoard(t *testing.T) {
	de25Path := filepath.Join("..", "..", "packages", "board", "de25_nano.yaml")
	if id, err := validatePath(de25Path); err != nil || id != "de25_nano" {
		t.Fatalf("validate DE25 board = %q, %v", id, err)
	}
	path := filepath.Join("..", "..", "packages", "board", "de10_nano.yaml")
	id, err := validatePath(path)
	if err != nil || id != "de10_nano" {
		t.Fatalf("validate board = %q, %v", id, err)
	}
	if err := reportPath(path); err != nil {
		t.Fatal(err)
	}
	for name, emit := range map[string]func(string) (string, error){
		"C++": emitPath, "Go": emitGoPath, "Verilog": emitVerilogPath,
	} {
		if _, err := emit(path); err == nil {
			t.Errorf("standalone board unexpectedly emitted %s", name)
		}
	}
}

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
	// C++ carries the constants a runtime reads, never ABI identity or capabilities.
	cpp, err := emitPath(path)
	if err != nil || !strings.Contains(cpp, "FesVideoPartRequestBits = 0x20u") ||
		strings.Contains(cpp, "ABIID") || strings.Contains(cpp, "Capability") {
		t.Fatalf("emit-cpp fabric = %q, %v", cpp, err)
	}
}
