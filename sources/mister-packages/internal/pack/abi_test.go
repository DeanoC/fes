package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadABIFesSimpleGameContract(t *testing.T) {
	root := repoRoot(t)
	abi, err := LoadABI(filepath.Join(root, "packages", "abi", "fes_simple_game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if abi.ID != "fes.simple-game" || abi.Major != 1 || abi.Minor != 0 || abi.Tag != 1 {
		t.Fatalf("ABI = %#v", abi)
	}
	for name, want := range map[string]uint32{
		"FesGpSignature":      0xf5000000,
		"FesGpRequestMask":    0x80000000,
		"FesGpAckMask":        0x00800000,
		"FesGpErrorMask":      0x00400000,
		"FesGpIdentityMagic0": 0x4546,
		"FesGpIdentityMagic1": 0x3153,
		"FesGpOpcodeIdentity": 1,
		"FesGpOpcodeGameplay": 2,
		"FesGpOpcodeButtons":  3,
		"FesGpButtonUp":       0x01,
		"FesGpButtonDown":     0x02,
		"FesGpButtonLeft":     0x04,
		"FesGpButtonRight":    0x08,
		"FesGpButtonA":        0x10,
		"FesGpButtonB":        0x20,
		"FesGpButtonSelect":   0x40,
		"FesGpButtonStart":    0x80,
	} {
		got, ok := abi.Constant(name)
		if !ok || got != want {
			t.Errorf("constant %s = 0x%x, %t; want 0x%x", name, got, ok, want)
		}
	}
	if len(abi.Interfaces) != 4 {
		t.Fatalf("interfaces = %#v", abi.Interfaces)
	}
	if abi.Interfaces[0].ID != "fes.gamepad" || abi.Interfaces[0].CapabilityBit != 0 {
		t.Fatalf("gamepad interface = %#v", abi.Interfaces[0])
	}
	if abi.Interfaces[1].ID != "fes.video.fixed-720p60" || abi.Interfaces[1].CapabilityBit != 1 {
		t.Fatalf("video interface = %#v", abi.Interfaces[1])
	}
}

func TestLoadABIFesSimpleComputerContract(t *testing.T) {
	root := repoRoot(t)
	abi, err := LoadABI(filepath.Join(root, "packages", "abi", "fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if abi.ID != "fes.simple-computer" || abi.Major != 1 || abi.Minor != 0 || abi.Tag != 2 {
		t.Fatalf("ABI = %#v", abi)
	}
	for name, want := range map[string]uint32{
		"FesSimpleComputerSignature":          0xf5000000,
		"FesSimpleComputerRequestMask":        0x80000000,
		"FesSimpleComputerAckMask":            0x00800000,
		"FesSimpleComputerErrorMask":          0x00400000,
		"FesSimpleComputerIdentityMagic0":     0x4546,
		"FesSimpleComputerIdentityMagic1":     0x3153,
		"FesSimpleComputerAbiTag":             2,
		"FesSimpleComputerOpcodeIdentity":     1,
		"FesSimpleComputerOpcodeExecution":    2,
		"FesSimpleComputerOpcodeKeyboard":     3,
		"FesSimpleComputerOpcodeMediaBegin":   4,
		"FesSimpleComputerOpcodeMediaData":    5,
		"FesSimpleComputerOpcodeMediaCommit":  6,
		"FesSimpleComputerKeyboardRowCount":   8,
		"FesSimpleComputerKeyboardNeutralRow": 0x1f,
		"FesSimpleComputerMediaMaxBytes":      16384,
		"FesSimpleComputerErrorInvalidState":  4,
	} {
		got, ok := abi.Constant(name)
		if !ok || got != want {
			t.Errorf("constant %s = 0x%x, %t; want 0x%x", name, got, ok, want)
		}
	}
	if len(abi.Interfaces) != 4 {
		t.Fatalf("interfaces = %#v", abi.Interfaces)
	}
	if abi.Interfaces[0].ID != "fes.keyboard" || abi.Interfaces[0].CapabilityBit != 0 {
		t.Fatalf("keyboard interface = %#v", abi.Interfaces[0])
	}
	if abi.Interfaces[1].ID != "fes.video.fixed-720p60" || abi.Interfaces[1].CapabilityBit != 1 {
		t.Fatalf("video interface = %#v", abi.Interfaces[1])
	}
	if abi.Interfaces[2].ID != "fes.media.blob" || abi.Interfaces[2].CapabilityBit != 2 {
		t.Fatalf("media interface = %#v", abi.Interfaces[2])
	}
}

func TestLoadABIMisterHasNoFabricTag(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "mister.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if abi.ID != "mister" || abi.Major != 1 || abi.Tag != 0 {
		t.Fatalf("ABI = %#v", abi)
	}
}

func TestLoadABIRejectsDuplicateAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	contents := `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
tag: 1
constants:
  - name: FesGpOne
    value: 1
  - name: FesGpOne
    value: 2
interfaces: []
unknown: rejected
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadABI(path)
	if err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("LoadABI error = %v, want strict unknown-field error", err)
	}
}

func TestLoadABIRejectsDuplicateConstantsAndCapabilityRange(t *testing.T) {
	for name, contents := range map[string]string{
		"duplicate": `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants:
  - name: FesGpOne
    value: 1
  - name: FesGpOne
    value: 2
interfaces: []
`,
		"capability-range": `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants: []
interfaces:
  - id: fes.gamepad
    major: 1
    minor: 0
    capability_bit: 32
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.yaml")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadABI(path); err == nil {
				t.Fatal("LoadABI accepted an invalid ABI")
			}
		})
	}
}

func TestLoadABIRejectsInterfaceDuplicatesAndUint32Overflow(t *testing.T) {
	for name, contents := range map[string]string{
		"duplicate-interface-id": `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants: []
interfaces:
  - id: fes.gamepad
    major: 1
    minor: 0
    capability_bit: 0
  - id: fes.gamepad
    major: 1
    minor: 0
    capability_bit: 1
`,
		"duplicate-capability-bit": `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants: []
interfaces:
  - id: fes.gamepad
    major: 1
    minor: 0
    capability_bit: 0
  - id: fes.video.fixed-720p60
    major: 1
    minor: 0
    capability_bit: 0
`,
		"uint32-overflow": `schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants:
  - name: FesGpOverflow
    value: 0x100000000
interfaces: []
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.yaml")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadABI(path); err == nil {
				t.Fatal("LoadABI accepted invalid interface or constant")
			}
		})
	}
}

func TestLoadABIRejectsSecondYAMLDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	contents := `schema: mister-packages.v1
kind: abi
id: mister
major: 1
minor: 0
constants: []
interfaces: []
---
schema: mister-packages.v1
kind: abi
id: fes.simple-game
major: 1
minor: 0
constants: []
interfaces: []
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadABI(path); err == nil {
		t.Fatal("LoadABI accepted multiple YAML documents")
	}
}

func TestLoadProgrammingProfilesKeepsDiagnosticProfileUnpaired(t *testing.T) {
	profiles, err := LoadProgrammingProfiles(filepath.Join(repoRoot(t), "packages", "programming", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if profiles.Platform != "de10_nano" || profiles.Device != "5CSEBA6U23I7" {
		t.Fatalf("registry target = %#v", profiles)
	}
	if len(profiles.Profiles) != 3 {
		t.Fatalf("profiles = %#v", profiles.Profiles)
	}
	for _, profile := range profiles.Profiles {
		switch profile.ID {
		case "mister-v1":
			if len(profile.ABIs) != 1 || profile.ABIs[0].ID != "mister" || profile.ABIs[0].Major != 1 || profile.DiagnosticOnly {
				t.Fatalf("mister profile = %#v", profile)
			}
		case "fes-gp-v1":
			if profile.DiagnosticOnly || len(profile.ABIs) != 3 {
				t.Fatalf("FES GP profile = %#v", profile)
			}
			if profile.ABIs[0].ID != "fes.simple-game" || profile.ABIs[0].Major != 1 {
				t.Fatalf("FES GP Pong pairing = %#v", profile.ABIs[0])
			}
			if profile.ABIs[1].ID != "fes.simple-computer" || profile.ABIs[1].Major != 1 {
				t.Fatalf("FES GP computer pairing = %#v", profile.ABIs[1])
			}
			if profile.ABIs[2].ID != "fes.application" || profile.ABIs[2].Major != 1 {
				t.Fatalf("FES GP application pairing = %#v", profile.ABIs[2])
			}
		case "development-contained-v1":
			if !profile.DiagnosticOnly || len(profile.ABIs) != 0 {
				t.Fatalf("diagnostic profile = %#v", profile)
			}
		default:
			t.Fatalf("unexpected profile %#v", profile)
		}
	}
}

func TestLoadProgrammingProfilesRejectsDiagnosticABIPair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	contents := `schema: mister-packages.v1
kind: programming_profiles
id: de10_nano
platform: de10_nano
device: 5CSEBA6U23I7
profiles:
  - id: development-contained-v1
    diagnostic_only: true
    abis:
      - id: mister
        major: 1
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProgrammingProfiles(path); err == nil {
		t.Fatal("diagnostic profile accepted an ABI pairing")
	}
}

func TestLoadProgrammingProfilesRejectsDuplicateAndUnpairedRows(t *testing.T) {
	for name, contents := range map[string]string{
		"duplicate-profile-id": `schema: mister-packages.v1
kind: programming_profiles
id: de10_nano
platform: de10_nano
device: 5CSEBA6U23I7
profiles:
  - id: mister-v1
    diagnostic_only: false
    abis: [{id: mister, major: 1}]
  - id: mister-v1
    diagnostic_only: false
    abis: [{id: fes.simple-game, major: 1}]
`,
		"duplicate-abi-major-pair": `schema: mister-packages.v1
kind: programming_profiles
id: de10_nano
platform: de10_nano
device: 5CSEBA6U23I7
profiles:
  - id: fes-gp-v1
    diagnostic_only: false
    abis:
      - id: fes.simple-game
        major: 1
      - id: fes.simple-game
        major: 1
`,
		"unpaired-normal-profile": `schema: mister-packages.v1
kind: programming_profiles
id: de10_nano
platform: de10_nano
device: 5CSEBA6U23I7
profiles:
  - id: mister-v1
    diagnostic_only: false
    abis: []
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.yaml")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProgrammingProfiles(path); err == nil {
				t.Fatal("LoadProgrammingProfiles accepted an invalid profile registry")
			}
		})
	}
}
