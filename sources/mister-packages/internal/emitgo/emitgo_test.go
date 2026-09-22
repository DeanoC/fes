package emitgo

import (
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateABIFesGpConstants(t *testing.T) {
	abi, err := pack.LoadABI("../../packages/abi/fes_simple_game.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text, err := GenerateABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateABI(abi)
	if err != nil {
		t.Fatal(err)
	}
	if text != again {
		t.Fatal("ABI Go generation is not deterministic")
	}
	computer, err := pack.LoadABI("../../packages/abi/fes_simple_computer.yaml")
	if err != nil {
		t.Fatal(err)
	}
	computerText, err := GenerateABI(computer)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"FesSimpleComputerABIID", `"fes.simple-computer"`, "FesSimpleComputerABITag", "uint16 = 2",
		"FesSimpleComputerSignature", "FesSimpleComputerCapabilityKeyboard",
		`FesSimpleComputerInterfaceKeyboardID`, `"fes.keyboard"`,
		`FesSimpleComputerInterfaceMediaBlobID`, `"fes.media.blob"`,
	} {
		if !strings.Contains(computerText, fragment) {
			t.Fatalf("missing computer ABI Go constant %q\n%s", fragment, computerText)
		}
	}
	for _, fragment := range []string{
		"FesGpABIID", `"fes.simple-game"`, "FesGpABIMajor", "uint16 = 1",
		"FesGpSignature", "uint32 = 0xf5000000", "FesGpCapabilityGamepad", "uint32 = 0x1",
		`FesGpInterfaceGamepadID`, `"fes.gamepad"`, "FesGpInterfaceGamepadMajor", "FesGpInterfaceGamepadMinor",
		`FesGpInterfaceVideoFixed720p60ID`, `"fes.video.fixed-720p60"`,
		"FesGpInterfaceVideoFixed720p60Major", "FesGpInterfaceVideoFixed720p60Minor", "FesGpCapabilityVideoFixed720p60",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("missing ABI Go constant %q\n%s", fragment, text)
		}
	}
}

func TestGenerateProgrammingProfilesPreservesDiagnosticWithoutABI(t *testing.T) {
	profiles, err := pack.LoadProgrammingProfiles("../../packages/programming/de10_nano.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text, err := GenerateProgrammingProfiles(profiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`Profile: "fes-gp-v1", ABI: "fes.simple-game", Major: 1`,
		`Profile: "fes-gp-v1", ABI: "fes.simple-computer", Major: 1`,
		`Profile: "development-contained-v1", DiagnosticOnly: true`,
		`De10NanoProgrammingPlatform = "de10_nano"`, "De10NanoProgrammingDevice", `"5CSEBA6U23I7"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("missing profile registry row %q\n%s", fragment, text)
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
