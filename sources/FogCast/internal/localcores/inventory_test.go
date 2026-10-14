package localcores

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
	"os"
	"path/filepath"
	"testing"
)

type coreOpts struct {
	rom          []byte
	romRole      string
	badROMHash   bool
	selectionSHA string
	cartridge    bool
	interfaces   []corepackage.Interface
}

func installCore(t *testing.T, packages, selections, coreID, name, abi string, opts coreOpts) string {
	t.Helper()
	payload := []byte("rbf:" + coreID)
	payloadSum := sha256.Sum256(payload)
	payloadSHA := hex.EncodeToString(payloadSum[:])
	dir := t.TempDir()
	format := 2
	role := opts.romRole
	if opts.rom != nil || opts.cartridge || role != "" {
		format = 3
		if role == "" {
			role = "firmware"
			if opts.cartridge {
				role = "cartridge"
			}
		}
	}
	manifest := fmt.Sprintf(`format = %d
[core]
id = %q
name = %q
description = "Synthetic local control test fixture"
version = "1.0.0"
[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/localcores"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "test fixture"
`, format, coreID, name, len(payload), payloadSHA, abi)
	interfaces := opts.interfaces
	if len(interfaces) == 0 {
		interfaces = []corepackage.Interface{{ID: "fes.video.fixed-720p60", Major: 1, Required: true}}
	}
	for _, iface := range interfaces {
		manifest += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = %d\nminor = %d\nrequired = %t\n", iface.ID, iface.Major, iface.Minor, iface.Required)
	}
	if format == 3 {
		// Bind a structurally valid test ROM map to these payload bytes. The marker
		// opts.rom selects a firmware requirement, not a physical bitstream fixture.
		file, err := os.Open("../../../misteross/expansion/testdata/rom/map.json.gz")
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		var mapping expansion.ROMMap
		if err := json.NewDecoder(reader).Decode(&mapping); err != nil {
			t.Fatal(err)
		}
		reader.Close()
		file.Close()
		mapping.BaseSHA256 = payloadSHA
		mapping.SourceSize = 1024
		mapping.Blocks = mapping.Blocks[:1]
		mapping.Blocks[0].SourceOffset = 0
		data, err := json.Marshal(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		manifest += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = %q\nsource_size = 1024\nfile = \"rom-map.json\"\nsize = %d\nsha256 = %q\n", role, len(data), fmt.Sprintf("%x", sha256.Sum256(data)))
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.rbf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := inspection.PackageID
	installed := filepath.Join(packages, id)
	if err := os.Rename(dir, installed); err != nil {
		t.Fatal(err)
	}
	dir = installed
	if opts.badROMHash {
		if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), []byte("corrupt"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	selectionSHA := payloadSHA
	if opts.selectionSHA != "" {
		selectionSHA = opts.selectionSHA
	}
	stem := coreID[len("fes."):]
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n",
		coreID, id, selectionSHA, dir)
	if err := os.WriteFile(filepath.Join(selections, "fes-"+stem+".package.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func byCore(cores []Core) map[string]Core {
	out := map[string]Core{}
	for _, core := range cores {
		out[core.CoreID] = core
	}
	return out
}

func TestReadInstalledCores(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "core-packages")
	selections := filepath.Join(root, "selections")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	rom := []byte(`{"format":1}` + "\n")
	menu := installCore(t, packages, selections, "fes.menu", "FES Menu", "fes.menu", coreOpts{})
	ramtest := installCore(t, packages, selections, "fes.ramtest", "FES RAM Tester", "fes.application", coreOpts{})
	pong := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	zx81 := installCore(t, packages, selections, "fes.zx81", "ZX81  (sealed)", "fes.simple-computer", coreOpts{rom: rom})
	coleco := installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{cartridge: true})
	sms := installCore(t, packages, selections, "fes.sms", "FES Master System", "fes.simple-computer", coreOpts{cartridge: true})
	sg := installCore(t, packages, selections, "fes.sg1000", "FES SG-1000", "fes.simple-computer", coreOpts{cartridge: true})
	c64 := installCore(t, packages, selections, "fes.c64", "FES Commodore 64", "fes.computer", coreOpts{rom: rom, romRole: "firmware"})
	spectrum := installCore(t, packages, selections, "fes.spectrum", "FES ZX Spectrum", "fes.computer", coreOpts{rom: rom, romRole: "firmware"})
	wrong := sha256.Sum256([]byte("wrong"))
	mismatch := installCore(t, packages, selections, "fes.mismatch", "Mismatch", "fes.simple-game", coreOpts{selectionSHA: hex.EncodeToString(wrong[:])})

	got := byCore(ReadInstalledCores(selections, packages))
	if _, ok := got["fes.menu"]; ok {
		t.Fatal("fes.menu was listed")
	}
	if _, ok := got[mismatch]; ok {
		t.Fatal("sha mismatch was listed by package id key")
	}
	for _, id := range []string{menu, mismatch} {
		for _, core := range got {
			if core.PackageID == id {
				t.Fatalf("dropped package listed: %s %s", core.CoreID, id)
			}
		}
	}
	want := []struct {
		id, packageID, name, abi, needs, block string
		launchable                             bool
	}{
		{"fes.ramtest", ramtest, "FES RAM Tester", "fes.application", "none", "", true},
		{"fes.pong", pong, "FES Pong", "fes.simple-game", "none", "", true},
		{"fes.zx81", zx81, "ZX81  (sealed)", "fes.simple-computer", "firmware", "Needs firmware", false},
		{"fes.coleco", coleco, "FES Coleco", "fes.application", "media", "Needs a cartridge", false},
		{"fes.sms", sms, "FES Master System", "fes.simple-computer", "media", "Needs a cartridge", false},
		{"fes.sg1000", sg, "FES SG-1000", "fes.simple-computer", "media", "Needs a cartridge", false},
		{"fes.c64", c64, "FES Commodore 64", "fes.computer", "firmware", "Needs firmware", false},
		{"fes.spectrum", spectrum, "FES ZX Spectrum", "fes.computer", "firmware", "Needs firmware", false},
	}
	if len(got) != len(want) {
		t.Fatalf("cores %d: %+v", len(got), got)
	}
	for _, item := range want {
		core, ok := got[item.id]
		if !ok {
			t.Fatalf("missing %s", item.id)
		}
		if core.PackageID != item.packageID || core.Name != item.name || core.ABI != item.abi ||
			core.Needs != item.needs || core.Block != item.block || core.Launchable != item.launchable {
			t.Fatalf("%s = %+v", item.id, core)
		}
	}
}

func TestUnverifiedROMIsOmitted(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.zx81", "ZX81", "fes.simple-computer", coreOpts{
		rom:        []byte(`{"format":1}` + "\n"),
		badROMHash: true,
	})
	got := ReadInstalledCores(selections, packages)
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestC64WithoutFirmwareIsLaunchable(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.c64", "FES Commodore 64", "fes.computer", coreOpts{})
	installCore(t, packages, selections, "fes.spectrum", "FES ZX Spectrum", "fes.computer", coreOpts{})
	got := byCore(ReadInstalledCores(selections, packages))
	for _, id := range []string{"fes.c64", "fes.spectrum"} {
		core, ok := got[id]
		if !ok || core.Needs != "none" || !core.Launchable || core.Block != "" {
			t.Fatalf("%s %+v", id, core)
		}
	}
}

func TestLaunchRequirementsComeFromDescriptor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     int64
		abi        string
		abiMinor   int64
		role       string
		interfaces []corepackage.Interface
		needs      string
		launchable bool
	}{
		{name: "new standalone application", format: 2, abi: "fes.application", needs: "none", launchable: true},
		{name: "renamed cartridge core", format: 3, abi: "fes.application", role: "cartridge", needs: "media"},
		{name: "firmware", format: 3, abi: "fes.simple-computer", role: "firmware", needs: "firmware"},
		{name: "multiple ROM sources", format: 4, abi: "fes.application", needs: "firmware"},
		{name: "native output is mandatory", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.fabric.video.native-pixels", Major: 1}}, needs: "parts"},
		{name: "cartridge cannot bypass native output", format: 3, abi: "fes.application", role: "cartridge", interfaces: []corepackage.Interface{{ID: "fes.fabric.video.native-pixels", Major: 1}}, needs: "parts"},
		{name: "blob requires host delivery", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "cartridge cannot substitute for required blob", format: 3, abi: "fes.application", role: "cartridge", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer empty drives", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer spectrum tape starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.spectrum-tape", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer c64 disk starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.c64-disk", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer st floppy starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.atari-st-floppy", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer st empty-drive extensions", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{
			{ID: "fes.media.atari-st-floppy", Major: 1, Required: true},
			{ID: "fes.media.atari-st-floppy-write", Major: 1, Required: true},
			{ID: "fes.media.atari-st-floppy-geometry", Major: 1, Required: true},
		}, needs: "none", launchable: true},
		{name: "computer blob is not an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer blob stream is not an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob-stream", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer blob stays blocked beside an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{
			{ID: "fes.media.apple2-floppy", Major: 1, Required: true},
			{ID: "fes.media.blob", Major: 1, Required: true},
		}, needs: "unsupported"},
		{name: "future computer floppy is not empty startup", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 2, Required: true}}, needs: "unsupported"},
		{name: "future computer abi does not inherit empty drives", format: 2, abi: "fes.computer", abiMinor: 1, interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "optional computer blob", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1}}, needs: "none", launchable: true},
		{name: "optional media", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1}}, needs: "none", launchable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := corepackage.Descriptor{Format: tc.format, Core: corepackage.Core{ID: "fes.new-core", Name: "New core"}, Target: corepackage.Target{ProgrammingProfile: "fes-gp-v1"}, ABI: corepackage.Contract{ID: tc.abi, Major: 1, Minor: tc.abiMinor}, Interfaces: tc.interfaces}
			if tc.role != "" {
				d.ROM = &corepackage.ROM{Role: tc.role}
			}
			inspection := corepackage.Inspection{PackageID: fmt.Sprintf("%064x", 1), Descriptor: d}
			got := FromInspection(inspection, "/installed/package")
			if got.Needs != tc.needs || got.Launchable != tc.launchable || (got.Block == "") != tc.launchable || got.installPath != "/installed/package" || got.CoreID != d.Core.ID {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestInstalledIdentityAndHyphenatedCoreID(t *testing.T) {
	root := t.TempDir()
	packages, selections := filepath.Join(root, "pkgs"), filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	id := installCore(t, packages, selections, "fes.atari-st", "ST", "fes.computer", coreOpts{})
	got := ReadInstalledCores(selections, packages)
	if len(got) != 1 || got[0].CoreID != "fes.atari-st" {
		t.Fatalf("hyphenated core missing: %+v", got)
	}
	path := filepath.Join(packages, id, "manifest.toml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A name change preserves payload SHA but changes package identity.
	manifest = bytes.Replace(manifest, []byte(`name = "ST"`), []byte(`name = "Changed"`), 1)
	if err := os.WriteFile(path, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadInstalledCores(selections, packages); len(got) != 0 {
		t.Fatalf("stale package identity admitted: %+v", got)
	}
}
