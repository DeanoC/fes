package emitcpp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateContainsOracleNames(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	resolved, err := pack.LoadPlatform(filepath.Join(root, "packages", "platform", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := Generate(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"kFpgaStatusAddress",
		"kFpgaDataAddress",
		"kSpiGpiAddress",
		"kFpgaCoreReset",
		"HPS_REG_APERTURE_BASE_ADDR",
	} {
		if !strings.Contains(text, name) {
			t.Errorf("generated C++ missing %s", name)
		}
	}
	if !strings.Contains(text, "namespace generated") {
		t.Error("missing generated namespace")
	}
	if strings.Contains(text, "inline constexpr") {
		t.Error("generated C++ is not C++14 (inline constexpr)")
	}
}

// These altered identities are emitter fixtures, not hardware system packages.

func TestGenerateABIFesGpConstantsCompileAsCxx14(t *testing.T) {
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
		t.Fatal("ABI C++ generation is not deterministic")
	}
	for _, fragment := range []string{
		"FesGpSignature = 0xf5000000u", "FesGpRequestMask = 0x80000000u",
		"FesGpAckMask = 0x800000u", "FesGpErrorMask = 0x400000u",
		`FesGpInterfaceGamepadID = "fes.gamepad"`, "FesGpInterfaceGamepadMajor = 1u",
		"FesGpInterfaceGamepadMinor = 0u", "FesGpCapabilityGamepad = 0x1u",
		`FesGpInterfaceVideoFixed720p60ID = "fes.video.fixed-720p60"`,
		"FesGpInterfaceVideoFixed720p60Major = 1u", "FesGpInterfaceVideoFixed720p60Minor = 0u",
		"FesGpCapabilityVideoFixed720p60 = 0x2u",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing ABI constant %q\n%s", fragment, generated)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fes_gp.hpp"), []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
	source := `#include "fes_gp.hpp"
using namespace mister::native::generated;
static_assert(FesGpSignature == 0xf5000000u, "signature");
static_assert(FesGpButtonStart == 0x80u, "buttons");
static_assert(FesGpCapabilityGamepad == 0x1u, "gamepad");
static_assert(FesGpInterfaceGamepadID[0] == 'f', "interface id");
`
	if err := os.WriteFile(filepath.Join(dir, "test.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := os.Getenv("CXX")
	if compiler == "" {
		compiler = "c++"
	}
	if out, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", filepath.Join(dir, "test.cpp")).CombinedOutput(); err != nil {
		t.Fatalf("FES GP C++14 header: %v\n%s", err, out)
	}
}

func TestGenerateABIFesSimpleComputerConstantsCompileWithFesGp(t *testing.T) {
	game, err := pack.LoadABI("../../packages/abi/fes_simple_game.yaml")
	if err != nil {
		t.Fatal(err)
	}
	computer, err := pack.LoadABI("../../packages/abi/fes_simple_computer.yaml")
	if err != nil {
		t.Fatal(err)
	}
	gameText, err := GenerateABI(game)
	if err != nil {
		t.Fatal(err)
	}
	computerText, err := GenerateABI(computer)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"FesSimpleComputerSignature = 0xf5000000u",
		"FesSimpleComputerAbiTag = 0x2u",
		`FesSimpleComputerInterfaceKeyboardID = "fes.keyboard"`,
		"FesSimpleComputerCapabilityKeyboard = 0x1u",
		`FesSimpleComputerInterfaceMediaBlobID = "fes.media.blob"`,
		"FesSimpleComputerCapabilityMediaBlob = 0x4u",
		"FesSimpleComputerOpcodeMediaCommit = 0x6u",
	} {
		if !strings.Contains(computerText, fragment) {
			t.Fatalf("missing computer ABI constant %q\n%s", fragment, computerText)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fes_gp.hpp"), []byte(gameText), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fes_simple_computer.hpp"), []byte(computerText), 0600); err != nil {
		t.Fatal(err)
	}
	source := `#include "fes_gp.hpp"
#include "fes_simple_computer.hpp"
using namespace mister::native::generated;
static_assert(FesGpSignature == FesSimpleComputerSignature, "shared signature");
static_assert(FesGpAbiTag == 1u && FesSimpleComputerAbiTag == 2u, "distinct tags");
static_assert(FesSimpleComputerCapabilityKeyboard == 0x1u, "keyboard");
static_assert(FesSimpleComputerCapabilityMediaBlob == 0x4u, "media");
`
	if err := os.WriteFile(filepath.Join(dir, "test.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := os.Getenv("CXX")
	if compiler == "" {
		compiler = "c++"
	}
	if out, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", filepath.Join(dir, "test.cpp")).CombinedOutput(); err != nil {
		t.Fatalf("computer ABI C++14 header: %v\n%s", err, out)
	}
}

func TestGenerateProgrammingProfilesPreservesDiagnosticWithoutABI(t *testing.T) {
	profiles, err := pack.LoadProgrammingProfiles("../../packages/programming/de10_nano.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateProgrammingProfiles(profiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"fes-gp-v1", "fes.simple-game", 1, false`,
		`"fes-gp-v1", "fes.simple-computer", 1, false`,
		`"development-contained-v1", nullptr, 0, true`,
		`kDe10NanoProgrammingPlatform = "de10_nano"`, `kDe10NanoProgrammingDevice = "5CSEBA6U23I7"`,
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing programming profile row %q\n%s", fragment, generated)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "programming.hpp"), []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
	source := `#include "programming.hpp"
using namespace mister::native::generated;
static_assert(kDe10NanoProgrammingProfilePairCount == 4, "registry rows");
static_assert(kDe10NanoProgrammingProfilePairs[3].diagnostic_only, "diagnostic");
`
	if err := os.WriteFile(filepath.Join(dir, "test.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := os.Getenv("CXX")
	if compiler == "" {
		compiler = "c++"
	}
	if out, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", filepath.Join(dir, "test.cpp")).CombinedOutput(); err != nil {
		t.Fatalf("programming registry C++14 header: %v\n%s", err, out)
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
