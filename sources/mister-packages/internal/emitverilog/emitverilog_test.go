package emitverilog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateABIFesGpMacrosLintWithVerilator(t *testing.T) {
	abi, err := pack.LoadABI("../../packages/abi/fes_simple_game.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	if generated != again {
		t.Fatal("ABI Verilog generation is not deterministic")
	}
	for _, fragment := range []string{
		"`define FES_GP_SIGNATURE 32'hf5000000", "`define FES_GP_REQUEST_MASK 32'h80000000",
		"`define FES_GP_ACK_MASK 32'h00800000", "`define FES_GP_BUTTON_START 32'h00000080",
		"`define FES_GP_INTERFACE_GAMEPAD_MAJOR 32'h00000001",
		"`define FES_GP_INTERFACE_GAMEPAD_MINOR 32'h00000000",
		"`define FES_GP_INTERFACE_GAMEPAD_CAPABILITY_BIT 32'h00000000",
		"`define FES_GP_INTERFACE_GAMEPAD_CAPABILITY_MASK 32'h00000001",
		"`define FES_GP_INTERFACE_VIDEO_FIXED_720P60_MAJOR 32'h00000001",
		"`define FES_GP_INTERFACE_VIDEO_FIXED_720P60_MINOR 32'h00000000",
		"`define FES_GP_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_BIT 32'h00000001",
		"`define FES_GP_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK 32'h00000002",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing Verilog macro %q\n%s", fragment, generated)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fes_gp.vh"), []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
	source := "`include \"fes_gp.vh\"\nmodule constants; wire [31:0] signature = `FES_GP_SIGNATURE; wire [31:0] buttons = `FES_GP_BUTTON_START; wire [31:0] gamepad = `FES_GP_INTERFACE_GAMEPAD_CAPABILITY_MASK; endmodule\n"
	if err := os.WriteFile(filepath.Join(dir, "constants.v"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	verilator := os.Getenv("VERILATOR")
	if verilator == "" {
		verilator = "/home/deano/fes/out/dev/yosys-rbf/misteross/build/toolchain/install/bin/verilator"
	}
	if _, err := os.Stat(verilator); err != nil {
		t.Skipf("Verilator unavailable: %v", err)
	}
	if out, err := exec.Command(verilator, "--lint-only", "-Wall", "-Wno-DECLFILENAME", "-Wno-UNUSEDSIGNAL", filepath.Join(dir, "constants.v"), "-I"+dir).CombinedOutput(); err != nil {
		t.Fatalf("generated Verilog macros: %v\n%s", err, out)
	}
}

func TestGenerateABIFesSimpleComputerMacros(t *testing.T) {
	abi, err := pack.LoadABI("../../packages/abi/fes_simple_computer.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"`define FES_SIMPLE_COMPUTER_SIGNATURE 32'hf5000000",
		"`define FES_SIMPLE_COMPUTER_ABI_TAG 32'h00000002",
		"`define FES_SIMPLE_COMPUTER_OPCODE_KEYBOARD 32'h00000003",
		"`define FES_SIMPLE_COMPUTER_OPCODE_MEDIA_COMMIT 32'h00000006",
		"`define FES_SIMPLE_COMPUTER_INTERFACE_KEYBOARD_CAPABILITY_MASK 32'h00000001",
		"`define FES_SIMPLE_COMPUTER_INTERFACE_MEDIA_BLOB_CAPABILITY_MASK 32'h00000004",
		"`define FES_SIMPLE_COMPUTER_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK 32'h00000002",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing computer Verilog macro %q\n%s", fragment, generated)
		}
	}
}

func TestGenerateABIRejectsGeneratedSymbolCollisions(t *testing.T) {
	base, err := pack.LoadABI("../../packages/abi/fes_simple_game.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*pack.ABIFile){
		"normalized-interface": func(abi *pack.ABIFile) {
			abi.Interfaces = append(abi.Interfaces, pack.ABIInterface{ID: "gamepad", Major: 1, CapabilityBit: 2})
		},
		"constant-interface": func(abi *pack.ABIFile) {
			abi.Constants = append(abi.Constants, pack.ABIConstant{Name: "FesGpInterfaceGamepadMajor", Value: 1})
		},
	} {
		t.Run(name, func(t *testing.T) {
			abi := *base
			abi.Constants = append([]pack.ABIConstant(nil), base.Constants...)
			abi.Interfaces = append([]pack.ABIInterface(nil), base.Interfaces...)
			mutate(&abi)
			if _, err := GenerateABI(&abi); err == nil {
				t.Fatal("GenerateABI accepted colliding generated symbols")
			}
		})
	}
}
