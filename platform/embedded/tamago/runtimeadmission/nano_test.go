// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	c "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func nanoDrivers(t *testing.T) (*Compatibility, []Driver) {
	t.Helper()
	registry, err := NewNanoComputerCompatibility()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.profiles) != 1 || registry.profiles[0].ABI != c.FesComputerABIID {
		t.Fatal("unexpected native computer registry")
	}
	return registry, []Driver{{Profile: registry.profiles[0].Profile, ABI: Contract{c.FesComputerABIID, 1, 0}, Interfaces: []Contract{
		{c.FesComputerInterfaceVideoFixed720p60ID, 1, 0}, {c.FesComputerInterfaceKeyboardHidID, 1, 0},
		{c.FesComputerInterfaceMediaZx81TapeID, 1, 0}, {"fes.embedded.rom", 1, 0},
	}}}
}

func retainedNanoComputer(t *testing.T, size int, change func(string) string) *corepackage.Archive {
	t.Helper()
	root := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	manifest, err := os.ReadFile(filepath.Join(root, "manifests", fmt.Sprintf("valid-nano-%d.toml", size)))
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := nanoDrivers(t)
	text := strings.ReplaceAll(string(manifest), "fes-gp-v1", registry.profiles[0].Profile)
	text = strings.Replace(text, "[abi]\nid = \"fes.simple-game\"", "[abi]\nid = \"fes.computer\"", 1)
	text = strings.Replace(text, "[[interfaces]]\nid = \"fes.gamepad\"\nmajor = 1\nminor = 0\nrequired = true\n", "", 1)
	if change != nil {
		text = change(text)
	}
	dir := t.TempDir()
	for name, original := range map[string]string{"core.rbf": "payloads/fes-fixture.rbf", "rom-map.json": fmt.Sprintf("maps/valid-nano-%d.json", size)} {
		data, err := os.ReadFile(filepath.Join(root, original))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNanoComputerRetainedROMRequiresPlatformDriverAndNativeRegistry(t *testing.T) {
	for _, size := range []int{4096, 8192} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			a := retainedNanoComputer(t, size, nil)
			id := a.Inspection().PackageID
			registry, drivers := nanoDrivers(t)
			combined, err := NewRuntimeCompatibility(registry, drivers)
			if err != nil || combined.CheckArchive(a, id) != nil {
				t.Fatal("native computer contract rejected", err)
			}
			linked, err := a.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: a.Inspection().Descriptor.ROM.ID, Role: "firmware", Data: bytes.Repeat([]byte{0xc3}, size)}})
			if err != nil || linked.Receipt().ROMLink == nil || linked.Receipt().ROMLink.ProgrammedSHA256 != a.Inspection().Descriptor.Payload.SHA256 {
				t.Fatal("native held ROM changed FPGA programming bytes", err)
			}
			de10, err := NewDE10Compatibility()
			if err != nil || ErrorCode(de10.CheckArchive(a, id)) != "unsupported_target" {
				t.Fatal("native contract escaped into DE10 registry", err)
			}
			drivers[0].Interfaces = drivers[0].Interfaces[:3]
			missing, err := NewRuntimeCompatibility(registry, drivers)
			if err != nil || ErrorCode(missing.CheckArchive(a, id)) != "unsupported_interface" {
				t.Fatal("ROM requirement supplied its own driver", err)
			}
		})
	}
}

func TestNanoComputerRejectsDiagnosticProfileNewMinorAndMissingVideo(t *testing.T) {
	for _, test := range []struct {
		name, code string
		change     func(string) string
	}{
		{"diagnostic", "unsupported_programming_profile", func(s string) string {
			return strings.ReplaceAll(s, "de25-native-computer-v1", "de25-embedded-hps-first-rom-diagnostic-v1")
		}},
		{"minor-one", "unsupported_abi", func(s string) string {
			return strings.Replace(s, "[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 0", "[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 1", 1)
		}},
		{"missing-video", "unsupported_interface", func(s string) string {
			return strings.Replace(s, "id = \"fes.video.fixed-720p60\"", "id = \"test.unavailable-video\"", 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := retainedNanoComputer(t, 8192, test.change)
			registry, drivers := nanoDrivers(t)
			checker, err := NewRuntimeCompatibility(registry, drivers)
			if err != nil {
				t.Fatal(err)
			}
			if got := ErrorCode(checker.CheckArchive(a, a.Inspection().PackageID)); got != test.code {
				t.Fatalf("got %s want %s", got, test.code)
			}
		})
	}
}

func TestHeldROMExtensionDoesNotBroadenDE10OrROMlessNanoAdmission(t *testing.T) {
	for _, required := range []bool{false, true} {
		a := fixture(t, func(s string) string {
			s = strings.Replace(s, "id = \"fes.simple-game\"", "id = \"fes.computer\"", 1)
			s = strings.Replace(s, "[[interfaces]]\nid = \"fes.gamepad\"\nmajor = 1\nminor = 0\nrequired = true\n", "", 1)
			return s + fmt.Sprintf("\n[[interfaces]]\nid = \"fes.embedded.rom\"\nmajor = 1\nminor = 0\nrequired = %t\n", required)
		})
		c, err := NewDE10Compatibility()
		if err != nil {
			t.Fatal(err)
		}
		want := "none"
		if required {
			want = "unsupported_interface"
		}
		if got := ErrorCode(c.CheckArchive(a, a.Inspection().PackageID)); got != want {
			t.Fatalf("required=%t got=%s want=%s", required, got, want)
		}
	}
	registry, drivers := nanoDrivers(t)
	a := fixture(t, func(s string) string {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "de10_nano", registry.target.Platform), "5CSEBA6U23I7", registry.target.Device)
		s = strings.ReplaceAll(s, "fes-gp-v1", registry.profiles[0].Profile)
		return strings.Replace(s, "id = \"fes.simple-game\"", "id = \"fes.computer\"", 1)
	})
	checker, err := NewRuntimeCompatibility(registry, drivers)
	if err != nil || ErrorCode(checker.CheckArchive(a, a.Inspection().PackageID)) != "unsupported_abi" {
		t.Fatal("ROMless archive entered the held-ROM programming profile", err)
	}
}

// The explicit producer test supplies metadata made by the actual producer,
// including its pre-synthesis build record. Payloads are synthetic, never RBFs.
func TestNanoComputerProducerMetadataAndROMConsumption(t *testing.T) {
	root := os.Getenv("FES_NANO_COMPUTER_FIXTURES")
	if root == "" {
		t.Skip("run test_nano_computer_package.py with its producer fixture output")
	}
	for _, model := range []string{"zx80", "zx81"} {
		t.Run(model, func(t *testing.T) {
			dir := filepath.Join(root, model)
			body, err := corepackage.CanonicalArchive(dir)
			if err != nil {
				t.Fatal(err)
			}
			a, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			d := a.Inspection().Descriptor
			if d.Core.ID != "fes."+model || d.ABI.ID != c.FesComputerABIID || len(d.Interfaces) != 4 {
				t.Fatal("producer did not describe its selected computer")
			}
			registry, drivers := nanoDrivers(t)
			checker, err := NewRuntimeCompatibility(registry, drivers)
			if err != nil || checker.CheckArchive(a, a.Inspection().PackageID) != nil {
				t.Fatal("actual producer metadata differs from native driver contract", err)
			}
			linked, err := a.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: bytes.Repeat([]byte{0x76}, int(d.ROM.SourceSize))}})
			if err != nil || !linked.Receipt().ROMLink.ValidFor(d) || linked.Receipt().ROMLink.ProgrammedSHA256 != d.Payload.SHA256 {
				t.Fatal("actual producer map or retained receipt differs", err)
			}
		})
	}
}
